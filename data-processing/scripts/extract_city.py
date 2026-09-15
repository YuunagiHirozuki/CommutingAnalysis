"""
extract_city.py — CPTOND-2025 通用城市数据提取/清洗脚本

从 CPTOND-2025 解压目录中提取指定城市的公交/地铁站点与站间连接，
生成 Go 后端加载的标准化图数据：

    nodes.json
    edges.json
    city.json

支持：
    1. 单个城市
    2. 多个城市批量处理
    3. 自动根据城市代码生成输出目录
    4. 自动计算城市 bbox
    5. 任何城市通用，无城市硬编码

用法：

    # 单个城市 + 自动输出目录
    python scripts/extract_city.py ../data/raw/unpacked \
        --city Guangzhou \
        --out-root ../data

    # 多个城市批量处理
    python scripts/extract_city.py ../data/raw/unpacked \
        --city Guangzhou Foshan Shenzhen \
        --out-root ../data

    # 兼容旧用法：指定单个输出目录
    python scripts/extract_city.py ../data/raw/unpacked \
        --city Guangzhou \
        --out ../data/guangzhou

新城市接入流程：

    1. 准备/解压目标城市原始数据
    2. 首次接触新数据版本时，可运行 inspect_data.py 检查结构
    3. 运行本脚本提取城市数据
    4. Go 后端自动扫描 data/ 下的城市目录

数据结构（以实际解压数据为准）：

    <City>/{prefix}_{bus|metro}_stops_unique.shp
        Point
        字段：
            stop_cn
            stop_en
            stop_id
            num
            city_cn

    <City>/{prefix}_{bus|metro}_segments.shp
        LineString
        字段：
            s_stopid
            e_stopid
            distance
            city_cn

清洗规则：

    - distance <= 0：
        使用 WGS84 测地线计算几何长度进行修复
    - distance > --max-edge-m：
        认为异常并丢弃
    - 自环：
        丢弃
    - 无法在节点表找到起点/终点：
        丢弃
    - 节点 stop_id：
        去重
    - 节点 city：
        优先使用数据中的 city_cn，
        缺失时回退到 --city

输出：

    <out-root>/<city-code>/
        city.json
        nodes.json
        edges.json
"""

import argparse
import json
import math
from pathlib import Path

import geopandas as gpd
import pandas as pd
from pyproj import Geod


GEOD = Geod(ellps="WGS84")

MODES = ("bus", "metro")

SUFFIX_STOPS = "_stops_unique.shp"
SUFFIX_SEGMENTS = "_segments.shp"


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser(
        description=(
            "从 CPTOND-2025 解压数据提取指定城市，"
            "生成 Go 后端加载的标准化图数据"
        )
    )


    city_group = ap.add_mutually_exclusive_group(required=True)
    city_group.add_argument(
        "--city",
        nargs="+",
        help=(
            "城市目录名，可一次指定一个或多个城市，"
            "例如 Guangzhou Foshan Shenzhen"
        )
    )
    city_group.add_argument(
        "--all",
        action="store_true",
        help="自动发现并处理所有城市"
    )

    ap.add_argument(
        "root",
        type=Path,
        help="CPTOND 解压根目录",
    )

    output_group = ap.add_mutually_exclusive_group(required=True)
    output_group.add_argument(
        "--out-root",
        type=Path,
        help=(
            "所有城市的输出根目录，"
            "例如 ../data；程序会自动生成 ../data/<city>"
        ),
    )

    output_group.add_argument(
        "--out",
        type=Path,
        help=(
            "兼容旧用法：仅允许 --city 指定一个城市时使用，"
            "直接指定该城市输出目录"
        ),
    )

    ap.add_argument(
        "--max-edge-m",
        type=float,
        default=10000,
        help="站间距离上限（米），超过视为异常丢弃，默认 10000",
    )

    return ap.parse_args()

def normalize_city_code(city: str) -> str:
    """
    将城市名转换成稳定的目录/code形式。

    当前数据中直接使用英文城市名即可：
        Guangzhou -> guangzhou
        Foshan    -> foshan

    这里只做大小写规范化和空白清理。
    """
    return city.strip().lower()


def find_city_files(
    root: Path,
    city: str,
) -> dict[str, dict[str, Path]]:
    """
    在解压树中定位城市目录下的站点/连接文件。

    匹配规则：
        shapefile 的直接父目录名 == city（忽略大小写）

    文件前缀从实际文件名推导，
    不假设任何固定城市前缀。

    返回：

        {
            "bus": {
                "stops": Path(...),
                "segments": Path(...)
            },
            "metro": {
                ...
            }
        }

    某个交通模式缺失时，不加入结果。
    """

    found: dict[str, dict[str, Path]] = {}

    city_lc = city.strip().lower()

    for stops in sorted(root.rglob(f"*{SUFFIX_STOPS}")):
        if stops.parent.name.lower() != city_lc:
            continue

        stem = stops.name[: -len(SUFFIX_STOPS)]

        # 例如：
        # xxx_bus_stops_unique.shp
        # stem = xxx_bus
        mode = stem.rsplit("_", 1)[-1].lower()

        if mode not in MODES:
            continue

        segments = stops.parent / f"{stem}{SUFFIX_SEGMENTS}"

        if not segments.exists():
            print(
                f"[warn] {city}: 找到 {stops.name} "
                f"但缺少 {segments.name}，跳过 {mode}",
                flush=True,
            )
            continue

        if mode in found:
            print(
                f"[warn] {city}: {mode} 数据出现多次，"
                f"使用 {stops.parent}",
                flush=True,
            )
            continue

        found[mode] = {
            "stops": stops,
            "segments": segments,
        }

    return found


def safe_float(value) -> float | None:
    """
    将数值安全转换为 float。

    None / NaN / inf 返回 None。
    """
    if value is None:
        return None

    try:
        value = float(value)
    except (TypeError, ValueError):
        return None

    if not math.isfinite(value):
        return None

    return value


def get_city_name(row, fallback: str) -> str:
    """
    获取城市中文名。

    优先使用 city_cn；
    如果字段不存在或值为空，则回退到命令行传入的城市名。
    """

    city_name = row.get("city_cn")

    if city_name is None:
        return fallback

    if pd.isna(city_name):
        return fallback

    city_name = str(city_name).strip()

    if not city_name:
        return fallback

    return city_name


def calculate_bbox(nodes: list[dict]) -> list[float] | None:
    """
    根据 nodes.json 中的经纬度自动计算 bbox。

    返回：
        [min_lon, min_lat, max_lon, max_lat]

    如果没有有效节点，则返回 None。
    """

    valid_points: list[tuple[float, float]] = []

    for node in nodes:
        lon = safe_float(node.get("lon"))
        lat = safe_float(node.get("lat"))

        if lon is None or lat is None:
            continue

        valid_points.append((lon, lat))

    if not valid_points:
        return None

    lons = [p[0] for p in valid_points]
    lats = [p[1] for p in valid_points]

    return [
        round(min(lons), 6),
        round(min(lats), 6),
        round(max(lons), 6),
        round(max(lats), 6),
    ]


def extract_one_city(
    root: Path,
    city: str,
    out_dir: Path,
    max_edge_m: float,
) -> None:
    """
    提取并清洗一个城市。
    """

    print()
    print("=" * 72)
    print(f"[city] {city}")
    print(f"[out ] {out_dir}")
    print("=" * 72)

    files = find_city_files(root, city)

    if not files:
        print(
            f"[error] 未在 {root} 下找到城市 {city!r} 的数据。\n"
            "提示：站点文件须位于对应城市目录下，"
            "并使用 *_bus_stops_unique.shp / "
            "*_metro_stops_unique.shp 命名。",
            flush=True,
        )
        raise RuntimeError(f"找不到城市数据: {city}")

    missing = [m for m in MODES if m not in files]

    if missing:
        print(
            f"[warn] {city}: 缺少 {'/'.join(missing)} 数据，"
            "仅使用现有交通模式继续",
            flush=True,
        )

    nodes: list[dict] = []
    edges: list[dict] = []

    # 原始 stop_id -> 内部 int id
    id_map: dict[str, int] = {}

    # 用于检查跨模式/重复 stop_id
    duplicate_stop_ids = 0

    city_name = city

    # 读取站点
    for mode in MODES:
        if mode not in files:
            continue

        stops_path = files[mode]["stops"]

        print(
            f"[read] {mode} stops: {stops_path}",
            flush=True,
        )

        su = gpd.read_file(stops_path)

        if su.empty:
            print(
                f"[warn] {city}/{mode}: 站点文件为空",
                flush=True,
            )
            continue

        required_columns = {"stop_id"}

        missing_columns = required_columns - set(su.columns)

        if missing_columns:
            raise RuntimeError(
                f"{city}/{mode}: stops 缺少必要字段 "
                f"{sorted(missing_columns)}"
            )

        n_invalid = 0
        n_duplicate = 0
        n_added = 0

        for r in su.itertuples(index=False):
            sid = getattr(r, "stop_id", None)

            if sid is None or pd.isna(sid):
                n_invalid += 1
                continue

            sid = str(sid).strip()

            if not sid:
                n_invalid += 1
                continue

            if sid in id_map:
                duplicate_stop_ids += 1
                n_duplicate += 1
                continue

            geometry = getattr(r, "geometry", None)

            if geometry is None or geometry.is_empty:
                n_invalid += 1
                continue

            try:
                lon = float(geometry.x)
                lat = float(geometry.y)
            except (AttributeError, ValueError, TypeError):
                n_invalid += 1
                continue

            if not math.isfinite(lon) or not math.isfinite(lat):
                n_invalid += 1
                continue

            row_dict = r._asdict()

            city_value = row_dict.get("city_cn")

            if city_value is not None and not pd.isna(city_value):
                city_value = str(city_value).strip()
            else:
                city_value = ""

            if city_value and city_name == city:
                city_name = city_value

            node_id = len(nodes)

            id_map[sid] = node_id

            stop_cn = row_dict.get("stop_cn")

            if stop_cn is None or pd.isna(stop_cn):
                stop_cn = sid

            nodes.append(
                {
                    "id": node_id,
                    "name": str(stop_cn),
                    "lat": lat,
                    "lon": lon,
                    "type": mode,
                    "city": get_city_name(row_dict, city),
                }
            )

        n_added += 1

        print(
            f"[{mode}] 站点 {len(su)}，"
            f"有效新增 {n_added}，"
            f"重复 {n_duplicate}，"
            f"无效 {n_invalid}",
            flush=True,
        )

    if not nodes:
        raise RuntimeError(
            f"{city}: 没有得到任何有效站点，停止生成输出"
        )

    # 读取 segments
    for mode in MODES:
        if mode not in files:
            continue

        segments_path = files[mode]["segments"]

        print(
            f"[read] {mode} segments: {segments_path}",
            flush=True,
        )

        seg = gpd.read_file(segments_path)

        required_columns = {
            "s_stopid",
            "e_stopid",
            "distance",
        }

        missing_columns = required_columns - set(seg.columns)

        if missing_columns:
            raise RuntimeError(
                f"{city}/{mode}: segments 缺少必要字段 "
                f"{sorted(missing_columns)}"
            )

        n_fixed = 0
        n_drop = 0
        n_invalid = 0

        for row in seg.itertuples(index=False):
            row_dict = row._asdict()

            f = row_dict.get("s_stopid")
            t = row_dict.get("e_stopid")

            if f is None or pd.isna(f):
                n_invalid += 1
                continue

            if t is None or pd.isna(t):
                n_invalid += 1
                continue

            f = str(f).strip()
            t = str(t).strip()

            if not f or not t:
                n_invalid += 1
                continue

            # 自环
            if f == t:
                n_drop += 1
                continue

            # 起点/终点必须存在于节点表
            if f not in id_map or t not in id_map:
                n_drop += 1
                continue

            d_km = safe_float(row_dict.get("distance"))

            geometry = row_dict.get("geometry")

            if d_km is None:
                n_drop += 1
                continue

            # 原始距离异常，尝试使用几何长度修复
            if d_km <= 0:
                if geometry is None or geometry.is_empty:
                    n_drop += 1
                    continue

                try:
                    # WGS84 下用测地线重新计算
                    coords = list(geometry.coords)

                    if len(coords) < 2:
                        n_drop += 1
                        continue

                    total_m = 0.0

                    for p1, p2 in zip(coords[:-1], coords[1:]):
                        lon1, lat1 = p1[:2]
                        lon2, lat2 = p2[:2]

                        _, _, distance_m = GEOD.inv(
                            lon1,
                            lat1,
                            lon2,
                            lat2,
                        )

                        total_m += distance_m

                    d_km = total_m / 1000.0
                    n_fixed += 1

                except Exception:
                    n_drop += 1
                    continue

            d_m = d_km * 1000.0

            if not math.isfinite(d_m):
                n_drop += 1
                continue

            if d_m <= 0:
                n_drop += 1
                continue

            if d_m > max_edge_m:
                n_drop += 1
                continue

            edges.append(
                {
                    "from": id_map[f],
                    "to": id_map[t],
                    "distance_m": round(d_m, 1),
                    "mode": mode,
                }
            )

        print(
            f"[{mode}] segments {len(seg)}，"
            f"修复≤0距离 {n_fixed}，"
            f"无效 {n_invalid}，"
            f"丢弃 {n_drop}",
            flush=True,
        )

    # 计算 bbox
    bbox = calculate_bbox(nodes)

    if bbox is None:
        raise RuntimeError(
            f"{city}: 无法根据节点计算 bbox"
        )

    # 生成 city.json
    city_code = normalize_city_code(city)

    city_meta = {
        "code": city_code,
        "name": city_name,
        "bbox": bbox,
        "nodeCount": len(nodes),
        "edgeCount": len(edges),
        "modes": [
            mode for mode in MODES
            if mode in files
        ],
    }

    # 写文件
    out_dir.mkdir(
        parents=True,
        exist_ok=True,
    )

    nodes_path = out_dir / "nodes.json"
    edges_path = out_dir / "edges.json"
    city_path = out_dir / "city.json"

    nodes_path.write_text(
        json.dumps(
            nodes,
            ensure_ascii=False,
        ),
        encoding="utf-8",
    )

    edges_path.write_text(
        json.dumps(
            edges,
            ensure_ascii=False,
        ),
        encoding="utf-8",
    )

    city_path.write_text(
        json.dumps(
            city_meta,
            ensure_ascii=False,
            indent=2,
        ),
        encoding="utf-8",
    )

    print()
    print(
        f"[done] {city}: "
        f"节点 {len(nodes)}, "
        f"边 {len(edges)}"
    )
    print(f"       bbox = {bbox}")
    print(f"       输出 = {out_dir}")

def resolve_output_dir(
    args: argparse.Namespace,
    city: str,
) -> Path:
    """
    根据命令行参数决定某个城市的输出目录。
    """
    if args.out is not None:
        if len(args.city) != 1:
            raise SystemExit(
                "--out 只支持单个城市。"
                "批量处理请使用 --out-root。"
            )
        return args.out

    city_code = normalize_city_code(city)

    return args.out_root / city_code

def discover_cities(root: Path) -> list[str]:
    """
    自动发现解压数据中的所有城市。

    根据 *_stops_unique.shp 所在目录的直接父目录名确定城市，
    与 find_city_files() 的匹配规则保持一致。
    """
    cities: set[str] = set()

    for stops in sorted(root.rglob(f"*{SUFFIX_STOPS}")):
        city_dir = stops.parent

        if city_dir.is_dir():
            cities.add(city_dir.name)

    return sorted(cities, key=str.lower)

def main() -> None:
    args = parse_args()

    root = args.root.resolve()

    if not root.exists():
        raise SystemExit(
            f"解压根目录不存在: {root}"
        )

    if not root.is_dir():
        raise SystemExit(
            f"指定的 root 不是目录: {root}"
        )

    if args.max_edge_m <= 0:
        raise SystemExit(
            "--max-edge-m 必须大于 0"
        )

    # 确定要处理的城市
    if args.all:
        cities = discover_cities(root)

        if not cities:
            raise SystemExit(
                f"未发现任何城市数据: {root}"
            )
        
        print(
        f"[info] 自动发现 {len(cities)} 个城市: "
        + ", ".join(cities)
        )
    else:
        cities = args.city

    success = 0
    failed = 0

    for city in cities:
        city = city.strip()

        if not city:
            continue

        try:
            out_dir = resolve_output_dir(
                args,
                city,
            )

            extract_one_city(
                root=root,
                city=city,
                out_dir=out_dir,
                max_edge_m=args.max_edge_m,
            )

            success += 1

        except Exception as exc:
            failed += 1

            print(
                f"[error] {city}: {exc}",
                flush=True,
            )

    print()
    print("=" * 72)
    print(
        f"[summary] 成功 {success}，失败 {failed}"
    )
    print("=" * 72)

    if failed > 0:
        raise SystemExit(1)


if __name__ == "__main__":
    main()