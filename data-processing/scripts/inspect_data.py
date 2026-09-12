"""
inspect_data.py — CPTOND-2025 数据结构检查(Phase 0,城市无关)

用法:
    python scripts/inspect_data.py <解压后的数据目录>

作用:
    在编写任何处理逻辑之前,输出真实数据结构:
    - 文件清单(按城市)
    - 每个 shapefile 的字段、几何类型、坐标系、记录数
    - 数据内容抽样与缺失值/重复 ID/几何范围统计
    按 AGENTS.md 第五节的 14 项检查清单输出结论。
"""
import sys
from pathlib import Path

import geopandas as gpd


def main(root: Path) -> None:
    shp_files = sorted(root.rglob("*.shp"))
    print(f"=== 共找到 {len(shp_files)} 个 Shapefile ===\n")

    # 1. 文件清单
    for f in shp_files:
        print(f"  {f.relative_to(root)}")

    # 2. 逐文件结构
    for f in shp_files:
        rel = f.relative_to(root)
        print(f"\n=== {rel} ===")
        try:
            gdf = gpd.read_file(f)
        except Exception as e:
            print(f"  读取失败: {e}")
            continue
        print(f"  记录数: {len(gdf)}")
        print(f"  几何类型: {gdf.geometry.geom_type.unique().tolist()}")
        print(f"  坐标系: {gdf.crs}")
        print(f"  字段: {list(gdf.columns)}")
        print(f"  字段类型:\n{gdf.dtypes.to_string()}")
        print(f"  缺失值统计:\n{gdf.isna().sum().to_string()}")
        print(f"  前 3 行:\n{gdf.head(3).to_string()}")

        # 几何范围(判断数据覆盖的城市区域)
        try:
            b = gdf.total_bounds  # [minx, miny, maxx, maxy]
            print(f"  几何范围: lon[{b[0]:.4f}, {b[2]:.4f}] lat[{b[1]:.4f}, {b[3]:.4f}]")
        except Exception as e:
            print(f"  几何范围计算失败: {e}")

        # ID 重复检查(点数据)
        for col in ("stop_id", "STOP_ID", "id", "ID", "osm_id"):
            if col in gdf.columns:
                dup = gdf[col].duplicated().sum()
                print(f"  {col} 重复数: {dup}")
                print(f"  {col} 样例: {gdf[col].head(5).tolist()}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(1)
    main(Path(sys.argv[1]))
