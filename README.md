# 公共交通可达区域分析系统

基于 CPTOND-2025 公共交通网络数据,构建城市公共交通网络图模型,通过**多源 Dijkstra** 算法计算
目标位置在不同时间约束(30/45/60/90 分钟)下的公共交通**理论可达**站点,结合**空间网格化**方法
生成等时可达区域,通过 Vue3 + Leaflet 实现 Web 地图可视化。

> ⚠ 系统输出为**理论可达区域**(基于网络拓扑与平均速度模型估算),**不是实时导航结果**。
> 由于缺少实际道路网络数据，起点至站点的步行距离采用 Haversine 直线距离近似，因此与实际步行路线存在一定误差。

## 项目结构

```
CommutingAnalysis/
├── backend/                  Go 后端(Gin + 图算法)
│   ├── cmd/server/           入口
│   ├── internal/graph/       图模型 + 优先队列 + 多源 Dijkstra
│   ├── internal/geo/         Haversine / 网格空间索引 / GeoJSON
│   ├── internal/service/     图加载 + 可达区域引擎
│   ├── internal/api/         REST 路由
│   ├── internal/config/      配置加载
│   ├── config/config.json    运行参数(速度/网格/城市范围)
│   └── gen_testdata/         合成 TEST DATA 生成器
├── frontend/                 Vue3 + TS + Vite + Leaflet
├── data-processing/          Python 预处理(GeoPandas,城市通用)
│   └── scripts/
│       ├── inspect_data.py       数据结构检查(Phase 0,城市无关)
│       └── extract_city.py       任意城市提取 + 图数据生成(Phase 1)
├── data/
│   ├── raw/CPTOND-2025.zip   原始数据集(2.16GB,Figshare)
│   ├── <city>/               预处理产物
│   ├── city.json
│   ├── nodes.json
│   ├── edges.json
│   ├── network.geojson
│   └── test/                 TEST DATA(合成网络,后端回退用)
└── docs/                     data-analysis / architecture / algorithm
```

## 快速开始

### 1. 数据准备(已完成后可跳过)

```PowerShell
# CPTOND-2025 下载(Figshare, 2.16GB)
# https://figshare.com/articles/dataset/CPTOND-2025/29377427
```

**新增城市流程:**

```PowerShell
# 1. 从 CPTOND-2025.zip 解压目标城市文件到 data/raw/unpacked/
cd .\data-processing\ && pip install -r requirements.txt
# 2. 检查数据结构(城市无关)
cd .\data-processing\
python scripts/inspect_data.py ../data/raw/unpacked
# 3. 提取/清洗/生成图数据
# 单选
cd .\data-processing\
python scripts/extract_city.py ../data/raw/unpacked `
  --city Guangzhou `
  --out-root ../data
# 多选
cd .\data-processing\
python scripts/extract_city.py ../data/raw/unpacked `
  --city Guangzhou Foshan Shenzhen `
  --out-root ../data
# 全选
cd .\data-processing\
python scripts/extract_city.py ../data/raw/unpacked `
    --all `
    --out-root ../data
# 4.重启后端, 程序将自动扫描 data/ 下各城市目录，根据 city.json 自动生成城市配置
```

### 2. 启动

```PowerShell
cd .\backend\
go run ./cmd/server -config config/config.json
# 图加载完成: N 节点 / M 弧,耗时 Xms
```

```PowerShell
cd .\frontend\
pnpm install
pnpm run dev
# 打开 http://localhost:5173
```

或使用一键启动脚本

```PowerShell
./start.bat
```

### 3. 使用

> 说明：地名搜索(/api/geocode)代理 OpenStreetMap Nominatim 外部服务，需要外网访问；
> 如需代理请在 `backend/config/config.json` 的 `network.proxyURL` 配置。

1. 侧边栏选择城市
2. 地名搜索(如"广州塔")或地图单击选择起点
3. 选择最大时间 30/45/60/90 分钟
4. 点击「计算可达区域」,查看分色网格与入口站点
5. 可选显示公共交通线路网络、卫星图切换。

## 测试

```bash
cd backend
go test ./internal/...    # 图算法/网格/GeoJSON/配置/跨河等时圈/接驳边 全部单元测试
go run ./cmd/netcheck -config config/config.json   # 网络连通性诊断(分量/跨河/地标可达)
```

测试覆盖 AGENTS.md 要求的全部用例:A→B→C 最短路径、A→C=10(次优路排除)、
多源起点择优、maxTime 剪枝、walk 换乘惩罚、平行边去重、网格时间/分级/GeoJSON 格式。

## 文档

- [数据调查](docs/data-analysis.md) — CPTOND-2025 真实结构与 14 项检查结论
- [架构说明](docs/architecture.md)
- [算法说明](docs/algorithm.md) — 时间权重模型、多源 Dijkstra、空间网格法
