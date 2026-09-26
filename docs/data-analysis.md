# CPTOND-2025 广州数据调查(Phase 0 结论)

> 调查对象:CPTOND-2025(Figshare,CC-BY 4.0,Shapefile,WGS-84)
> 本文档由 `data-processing/scripts/inspect_data.py` 对真实数据的检查结果整理。
> 结论日期:2026-09-11

## 1. 数据组织

数据集为全国单 zip(2.16GB,8361 个文件),按 `{city_en}_{bus|metro}_{routes|segments|stops|stops_unique}.shp` 组织。
为减少空间，仅以广州为例:

```ps
CPTOND-2025/dataset/bus/shapefiles/Guangzhou/
    guangzhou_bus_routes.shp          2612 条线路(LineString)
    guangzhou_bus_segments.shp        26266 条站间连接(LineString)
    guangzhou_bus_stops.shp           62089 条 线路×站点 记录(Point,非物理站点)
    guangzhou_bus_stops_unique.shp    10143 个物理站点去重(Point)
CPTOND-2025/dataset/metro/shapefiles/Guangzhou/
    guangzhou_metro_routes.shp        线路
    guangzhou_metro_segments.shp        982 条站间连接
    guangzhou_metro_stops.shp         1062 条 线路×站点 记录
    guangzhou_metro_stops_unique.shp   409 个物理站点去重
```

## 2. 十四项检查结论

| #   | 检查项             | 结论                                                                                              |
| --- | ------------------ | ------------------------------------------------------------------------------------------------- |
| 1   | 广州公交数据位置   | `dataset/bus/shapefiles/Guangzhou/`(按城市名分目录)                                               |
| 2   | 广州地铁数据位置   | `dataset/metro/shapefiles/Guangzhou/`                                                             |
| 3   | 实际字段           | 见下表                                                                                            |
| 4   | 坐标系             | EPSG:4326(WGS-84),无需转换                                                                        |
| 5   | stop ID 类型       | 字符串 `BV########`(如 BV09199365),公交/地铁统一前缀                                              |
| 6   | segment 起终点字段 | `s_stopid` / `e_stopid`                                                                           |
| 7   | distance 单位      | **公里**(float;地铁均值 2.03、公交中位 0.64 → km)                                                 |
| 8   | 重复站点           | `stops_unique` 无重复(stop_id 唯一);`stops` 为线路×站点长表                                       |
| 9   | 公交/地铁 ID 冲突  | 无重叠(公交 10143 与地铁 409 的 BV ID 完全不相交)                                                 |
| 10  | 跨线路共享站点     | 存在,由 `stops_unique` 天然合并为图节点                                                           |
| 11  | 广佛跨城线路       | 广州包内 `city_cn` 全部为「广州」,无佛山站点                                                      |
| 12  | 数据量             | 公交站 10143 + 地铁站 409;segments 26266 + 982                                                    |
| 13  | 缺失值             | routes 的 company/start_time 等有缺失,**stops/segments 关键字段(stop_id/坐标/distance)无缺失**    |
| 14  | 异常距离           | 公交 179 条 distance≤0(取几何长度修复 9 条,其余多为自环/重复丢弃)、无 >5km 站间(仅线路全长字段超) |

## 3. 关键字段明细

**stops_unique**:`stop_cn`(中文名)、`stop_en`、`stop_id`(主键)、`num`(经停线路数)、`city_cn`
**segments**:`s_stopid`→`e_stopid`、`distance`(km)、`num`(重复次数)、几何(LineString,可用于修复距离)
**routes**:`route_cn`、`distance`(线路全长 km)、`total_stop` 等(本项目未用)

## 4. 完整性验证

- segments 引用的 stop_id 100% 存在于 stops_unique(公交/地铁均无悬挂引用)
- stops_unique 中 100% 的站点被 segments 引用(无孤立站点)
- 公交几何范围 lon[112.44,114.04] lat[22.53,23.92](含花都/从化/南沙等远郊区)
- 地铁几何范围 lon[113.10,113.81] lat[22.52,23.57](中心城区)

## 5. 处理决策(extract_city.py)

1. 节点 = `*_stops_unique`(10143+409 = 10552 个);字符串 BV ID 映射为 int64 序号
2. 边 = `*_segments`(s_stopid→e_stopid),distance km→米
3. distance≤0 → 用 UTM 49N 投影几何长度替代(公交修复 9 条)
4. 站间 >10km 或自环 → 丢弃(共 274 条)
5. 产出：  
    data/`<city>`/city.json  
    data/`<city>`/nodes.json  
    data/`<city>`/edges.json  
    data/`<city>`/network.geojson  
   nodes.json 与 edges.json 用于 Go 端图算法计算；network.geojson 保存原始站间线路几何，仅用于前端交通线路可视化。

## 6. Go 端建图结果(实测)

以下统计和处理结果以广州数据为例,用于验证提取脚本和图数据生成流程;同一流程可用于其他城市。

```
节点 10552 / 有向弧 34366(平行边去重后,无向边≈17183)
# 预处理生成公交/地铁线路边 26966 条；Go 端建图时进一步补充站点间 walk 接驳边，因此最终内存图包含 34366 条有向弧。
加载+建图耗时约 79ms
多源 Dijkstra(广州,60min,市中心起点):3502 可达站点,计算耗时约 25ms
```

## 7. 局限说明

- segments 是线路方向上的站对连接,同一物理站对的平行边由 Go `Build()` 去重取最短
- distance 为线路几何推算里程,非车轮实测里程
- 数据不含时刻表/班次,系统输出为平均速度模型下的**理论可达时间**
