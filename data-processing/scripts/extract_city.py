"""
extract_city.py — CPTOND-2025 通用城市数据提取/清洗脚本(Phase 1)

从 CPTOND-2025 解压目录中提取指定城市的站点与站间连接,
生成 Go 后端加载的标准化图数据(任何城市通用,无城市硬编码)。

用法:
    python scripts/extract_city.py <解压根目录> --city Guangzhou --out ../data/guangzhou
    python scripts/extract_city.py <解压根目录> --city Foshan --out ../data/foshan

新城市接入流程(零代码改动):
    1. 从 CPTOND-2025.zip 解压目标城市文件到 raw 数据目录
    2. python scripts/inspect_data.py <解压根目录>   # 确认结构
    3. python scripts/extract_city.py <根目录> --city Xxx --out data/xxx

数据结构(以实际解压数据为准,见 docs/data-analysis.md):
    <City>/{prefix}_{bus|metro}_stops_unique.shp   物理站点去重表(Point)
        字段: stop_cn, stop_en, stop_id(全局唯一), num, city_cn
    <City>/{prefix}_{bus|metro}_segments.shp       站间连接(LineString)
        字段: s_stopid, e_stopid, distance(公里), city_cn

清洗规则:
    - distance ≤ 0:用 WGS84 测地线几何长度修复(与城市位置无关)
    - distance > --max-edge-m(默认 10km)或自环:丢弃
    - 节点 city 字段优先取数据 city_cn,缺失时回退 --city
"""
import argparse
import json
from pathlib import Path

import geopandas as gpd
from pyproj import Geod

GEOD = Geod(ellps="WGS84")
MODES = ("bus", "metro")
SUFFIX_STOPS = "_stops_unique.shp"
SUFFIX_SEGMENTS = "_segments.shp"


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser(
        description="从 CPTOND-2025 解压数据提取指定城市,生成图数据 nodes.json/edges.json",
    )
    ap.add_argument("root", type=Path, help="CPTOND 解压根目录(内含 dataset/<bus|metro>/shapefiles/...)")
    ap.add_argument("--city", required=True, help="城市目录名,如 Guangzhou、Foshan、Shenzhen(忽略大小写)")
    ap.add_argument("--out", type=Path, required=True, help="输出目录(将写入 nodes.json 与 edges.json)")
    ap.add_argument("--max-edge-m", type=float, default=10000,
                    help="站间距离上限(米),超过视为异常丢弃,默认 10000")
    return ap.parse_args()


def find_city_files(root: Path, city: str) -> dict[str, dict[str, Path]]:
    """在解压树中定位城市目录下的站点/连接文件。

    匹配规则:shapefile 的直接父目录名 == city(忽略大小写);
    文件前缀从实际文件名推导(剥离 _{mode}_stops_unique / _{mode}_segments 后缀),
    不假设任何命名前缀。
    返回 {mode: {"stops": Path, "segments": Path}},模式缺失则不出现在结果中。
    """
    found: dict[str, dict[str, Path]] = {}
    city_lc = city.lower()
    for stops in sorted(root.rglob(f"*{SUFFIX_STOPS}")):
        if stops.parent.name.lower() != city_lc:
            continue
        stem = stops.name[: -len(SUFFIX_STOPS)]  # 如 guangzhou_bus
        mode = stem.rsplit("_", 1)[-1]           # bus | metro
        segments = stops.parent / f"{stem}{SUFFIX_SEGMENTS}"
        if not segments.exists():
            print(f"[warn] {city}: 找到 {stops.name} 但缺少 {segments.name},跳过 {mode}", flush=True)
            continue
        if mode in found:
            print(f"[warn] {city}: {mode} 数据出现多次,使用 {stops.parent}", flush=True)
            continue
        found[mode] = {"stops": stops, "segments": segments}
    return found


def main() -> None:
    args = parse_args()
    if not args.root.exists():
        raise SystemExit(f"解压根目录不存在: {args.root}")

    files = find_city_files(args.root, args.city)
    if not files:
        raise SystemExit(
            f"未在 {args.root} 下找到城市 {args.city!r} 的数据。\n"
            f"提示: 站点文件须形如 <CityDir>/{'<prefix>'}_bus_stops_unique.shp,"
            f"城市目录名须与 --city 一致(忽略大小写)。可先运行 inspect_data.py 确认结构。"
        )
    missing = [m for m in MODES if m not in files]
    if missing:
        print(f"[warn] {args.city}: 缺少 {'/'.join(missing)} 数据,仅使用现有模式继续", flush=True)

    nodes: list[dict] = []
    edges: list[dict] = []
    id_map: dict[str, int] = {}  # BV 字符串 ID → 全局 int64

    for mode in MODES:
        if mode not in files:
            continue
        su = gpd.read_file(files[mode]["stops"])
        for _, r in su.iterrows():
            sid = r["stop_id"]
            if sid in id_map:
                continue
            id_map[sid] = len(nodes)
            city_name = r.get("city_cn")
            nodes.append({
                "id": id_map[sid],
                "name": str(r["stop_cn"]),
                "lat": float(r.geometry.y),
                "lon": float(r.geometry.x),
                "type": mode,
                "city": str(city_name) if city_name else args.city,
            })

        seg = gpd.read_file(files[mode]["segments"])
        n_fixed = n_drop = 0
        for i, r in seg.iterrows():
            f, t = r["s_stopid"], r["e_stopid"]
            if f not in id_map or t not in id_map or f == t:
                n_drop += 1
                continue
            d_km = float(r["distance"])
            if d_km <= 0:
                d_km = GEOD.geometry_length(seg.geometry.iloc[i]) / 1000
                n_fixed += 1
            d_m = d_km * 1000
            if d_m > args.max_edge_m:
                n_drop += 1
                continue
            edges.append({
                "from": id_map[f],
                "to": id_map[t],
                "distance_m": round(d_m, 1),
                "mode": mode,
            })
        print(f"[{mode}] 站点 {len(su)},segments {len(seg)},修复≤0距离 {n_fixed},丢弃 {n_drop}", flush=True)

    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "nodes.json").write_text(json.dumps(nodes, ensure_ascii=False), encoding="utf-8")
    (args.out / "edges.json").write_text(json.dumps(edges, ensure_ascii=False), encoding="utf-8")
    print(f"[done] {args.city}: 节点 {len(nodes)},边 {len(edges)} → {args.out}")


if __name__ == "__main__":
    main()
