# 前端（公共交通等时圈分析系统 Web 端）

基于 Vue 3 + TypeScript + Vite + Leaflet 的单页应用，负责地图交互与结果渲染，所有交通网络计算均由 Go 后端完成。

## 功能

- 城市选择（列表来自后端 `GET /api/cities`，切换后自动定位到城市包围盒）
- 起点选择：地名搜索（`GET /api/geocode`，经后端代理 Nominatim）或地图单击取点
- 最大出行时间 30 / 45 / 60 / 90 分钟，触发 `POST /api/isochrone` 计算
- 等时圈 GeoJSON 按四级分色渲染（0-30 / 30-45 / 45-60 / 60-90 分钟，`preferCanvas` 保证大数据量流畅）
- 入口站点标注、可达公交/地铁线路展示开关、标准地图 / 卫星影像底图切换

## 启动

```powershell
pnpm install
pnpm run dev   # http://localhost:5173，需先启动 Go 后端(127.0.0.1:8080)
```

接口约定与系统架构见根目录 `docs/architecture.md`。
