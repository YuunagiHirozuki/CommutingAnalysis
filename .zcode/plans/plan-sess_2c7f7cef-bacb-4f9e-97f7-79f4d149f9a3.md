## 第一部分:右上角底图切换(前端)

`frontend/src/App.vue`:
- 地图容器右上角添加绝对定位下拉框,两个选项:标准地图(OpenStreetMap)、卫星影像(Esri World Imagery,`server.arcgisonline.com` 的 World_Imagery 瓦片,标注 Esri 版权)
- 维护单个当前底图层,切换时 removeLayer 旧层并加载新层;可达网格/站点标注在 overlay pane,自动保持在底图上方
- `npm run build` + 浏览器验证

## 第二部分:重构 `extract_guangzhou.py` → `extract_city.py`

新脚本 CLI:`extract_city.py <解压根目录> --city Guangzhou --out ../data/guangzhou [--max-edge-m 10000]`,argparse 提供 --help。

核心设计(对应 12 条要求):
1. **城市定位**:rglob 全部 shapefile,按"父目录名 == --city(忽略大小写)"筛选;文件前缀从实际文件名推导(剥离 `_{mode}_{stops_unique|segments}` 后缀),不假设任何前缀
2. **缺模式容错**:bus/metro 各自查找 `*_stops_unique.shp` 与 `*_segments.shp`,缺失时 [warn] 跳过该模式,只有公交也正常生成
3. **投影通用化**:删除 EPSG:32649,≤0 距离修复改用 `pyproj.Geod(ellps=WGS84).geometry_length()` 测地线长度,任何城市通用
4. **清洗逻辑保留**:km→m、≤0 修复、超长/自环丢弃、ID 映射,输出 nodes.json + edges.json
5. **city 字段**:优先数据 `city_cn`,回退 --city
6. 删除旧 `extract_guangzhou.py`

## 顺带清理
- `inspect_data.py` 删除 GZ_BBOX 及"与广州相交"判断,仅打印实际范围
- README/docs 更新新城市接入三步流程(解压 → inspect → extract,零代码改动)
- `gen_testdata`(TEST DATA 生成器,已明确标注)保留不动

## 验证
1. `--help` 正常
2. 实际运行广州数据:节点 10552/边 26974 数量一致,节点坐标逐点一致,边距离除 9 条修复边外完全相同(测地线 vs UTM 米级差异属预期)
3. 通过后替换 data/guangzhou,重启后端,health 确认 10552/34366 不变,isochrone 60min 仍 3502 站
4. 浏览器验证卫星图切换与网格叠加