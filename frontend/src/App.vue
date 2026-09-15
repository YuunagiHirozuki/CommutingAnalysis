<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import * as L from "leaflet";
import "leaflet/dist/leaflet.css";

interface City {
  code: string;
  name: string;
  bbox: [number, number, number, number];
  testData: boolean;
}
interface Station {
  id: number;
  name: string;
  type: string;
  lat: number;
  lon: number;
  distanceM: number;
  walkMin: number;
}
interface GeoResult {
  name: string;
  latitude: number;
  longitude: number;
}
interface IsochroneResp {
  city: string;
  center: { latitude: number; longitude: number };
  maxTime: number;
  reachableStations: number;
  entryStations: Station[];
  computeMs: number;
  gridCells: number;
  testData: boolean;
  notice?: string;
  network?: GeoJSON.FeatureCollection;
  geojson: GeoJSON.FeatureCollection;
}

const cities = ref<City[]>([]);
const city = ref<string>("");
const maxTime = ref<number>(60);
const computing = ref(false);
const errorMsg = ref("");
const result = ref<IsochroneResp | null>(null);
const picked = ref<{ lat: number; lon: number } | null>(null);

// 地址搜索
const query = ref("");
const searching = ref(false);
const geoResults = ref<GeoResult[]>([]);
const searchDone = ref(false);

// 与后端 grid.levels [30,45,60] 对应:0-30/30-45/45-60/60-max(90分钟档第4环)
const LEVEL_COLORS = ["#238b45", "#f1a22e", "#e05252", "#7c3aed"];

// 图例随"已计算结果"的时间档显示(而非当前选中值,避免切换后未重算时对不上)
const legend = computed(() => {
  const all = [
    { color: LEVEL_COLORS[0], label: "0~30 分钟", from: 0 },
    { color: LEVEL_COLORS[1], label: "30~45 分钟", from: 30 },
    { color: LEVEL_COLORS[2], label: "45~60 分钟", from: 45 },
    { color: LEVEL_COLORS[3], label: "60~90 分钟", from: 60 },
  ];
  const t = result.value?.maxTime ?? maxTime.value;
  return all.filter((item) => t > item.from);
});

let map: L.Map;
let marker: L.CircleMarker | null = null;
let gridLayer: L.GeoJSON | null = null;
let stationLayer: L.LayerGroup | null = null;

// 底图定义:标准 OSM 与 Esri 卫星影像
const BASEMAPS: Record<
  string,
  { name: string; url: string; attribution: string }
> = {
  osm: {
    name: "标准地图",
    url: "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
    attribution: "&copy; OpenStreetMap contributors",
  },
  esri: {
    name: "卫星影像",
    url: "https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}",
    attribution: "Tiles &copy; Esri",
  },
};
const basemap = ref<string>("osm");
let baseLayer: L.TileLayer | null = null;

// 交通线路展示开关(默认关):开启后请求可达子网络并渲染
const showNetwork = ref(false);
let networkLayer: L.GeoJSON | null = null;

// 线路按交通方式着色(与网格配色区分)
const MODE_COLORS: Record<string, { color: string; weight: number }> = {
  bus: { color: "#0ea5e9", weight: 1.5 },
  metro: { color: "#c026d3", weight: 3 },
};

function updateNetworkLayer(data: IsochroneResp | null) {
  if (networkLayer) {
    networkLayer.remove();
    networkLayer = null;
  }
  if (!data?.network || !showNetwork.value) return;
  networkLayer = L.geoJSON(data.network as unknown as GeoJSON.GeoJsonObject, {
    style: (f) => {
      const mode =
        ((f as GeoJSON.Feature).properties as { mode: string } | undefined)
          ?.mode ?? "bus";
      const s = MODE_COLORS[mode] ?? MODE_COLORS.bus;
      return { color: s.color, weight: s.weight, opacity: 0.75, fill: false };
    },
    onEachFeature: (f, layer) => {
      const mode =
        ((f as GeoJSON.Feature).properties as { mode: string } | undefined)
          ?.mode ?? "bus";
      layer.bindTooltip(mode === "metro" ? "地铁线路段" : "公交线路段");
    },
  }).addTo(map);
}

watch(showNetwork, (on) => {
  if (!on) {
    if (networkLayer) {
      networkLayer.remove();
      networkLayer = null;
    }
    return;
  }
  // 开启时若无网络数据(此前关闭状态下计算),用当前起点静默重算一次
  if (
    result.value &&
    !result.value.network &&
    picked.value &&
    !computing.value
  ) {
    compute();
  }
});

function switchBasemap(key: string) {
  const def = BASEMAPS[key];
  if (!def || !map) return;
  if (baseLayer) baseLayer.remove();
  baseLayer = L.tileLayer(def.url, {
    attribution: def.attribution,
    maxZoom: 19,
  }).addTo(map);
}

watch(basemap, switchBasemap);

onMounted(async () => {
  // preferCanvas:数千个网格多边形在单个画布上批量绘制,
  // 比 SVG 渲染器(每个多边形一个 DOM 节点)快一个数量级,消除 90 分钟档卡顿
  map = L.map("map", {
    center: [23.1291, 113.2644],
    zoom: 5,
    preferCanvas: true,
  });

  switchBasemap("osm");

  map.on("click", (e: L.LeafletMouseEvent) => {
    pick(e.latlng.lat, e.latlng.lng);
  });

  try {
    const resp = await fetch("http://127.0.0.1:8080/api/cities");
    const data = await resp.json();

    cities.value = data.cities;

    if (cities.value.length > 0) {
      city.value = cities.value[0].code;
      flyToCity(cities.value[0]);
    }
  } catch {
    errorMsg.value = "无法连接后端服务(127.0.0.1:8080),请先启动 Go 后端";
  }
});

watch(city, (code) => {
  const c = cities.value.find((x) => x.code === code);
  if (c) flyToCity(c);
  // 城市切换后清除不属于该城市的结果
  result.value = null;
  geoResults.value = [];
  searchDone.value = false;
});

function flyToCity(c: City) {
  const bounds = L.latLngBounds([c.bbox[1], c.bbox[0]], [c.bbox[3], c.bbox[2]]);

  map.fitBounds(bounds, {
    padding: [30, 30],
    animate: true,
    duration: 0.8,
  });
}

function pick(lat: number, lon: number) {
  picked.value = { lat, lon };
  errorMsg.value = "";
  if (marker) marker.remove();
  marker = L.circleMarker([lat, lon], {
    radius: 8,
    color: "#1d4ed8",
    fillColor: "#3b82f6",
    fillOpacity: 0.9,
    weight: 2,
  })
    .addTo(map)
    .bindTooltip("起点");
}

async function search() {
  const q = query.value.trim();
  if (!q) return;
  searching.value = true;
  searchDone.value = false;
  try {
    const resp = await fetch(
      `http://127.0.0.1:8080/api/geocode?city=${encodeURIComponent(city.value)}&q=${encodeURIComponent(q)}`,
    );
    if (!resp.ok) {
      const err = await resp
        .json()
        .catch(() => ({ error: `HTTP ${resp.status}` }));
      throw new Error(err.error ?? `HTTP ${resp.status}`);
    }
    const data = await resp.json();
    geoResults.value = data.results;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    searching.value = false;
    searchDone.value = true;
  }
}

function chooseResult(r: GeoResult) {
  pick(r.latitude, r.longitude);
  map.flyTo([r.latitude, r.longitude], 14, { duration: 0.8 });
}

async function compute() {
  if (!picked.value) {
    errorMsg.value = "请先在地图上点击或搜索选择起点";
    return;
  }
  computing.value = true;
  errorMsg.value = "";
  try {
    const resp = await fetch("http://127.0.0.1:8080/api/isochrone", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        city: city.value,
        latitude: picked.value.lat,
        longitude: picked.value.lon,
        maxTime: maxTime.value,
        includeNetwork: showNetwork.value,
      }),
    });
    if (!resp.ok) {
      const err = await resp
        .json()
        .catch(() => ({ error: `HTTP ${resp.status}` }));
      throw new Error(err.error ?? `HTTP ${resp.status}`);
    }
    const data: IsochroneResp = await resp.json();
    result.value = data;
    render(data);
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    computing.value = false;
  }
}

function render(data: IsochroneResp) {
  if (gridLayer) {
    gridLayer.remove();
    gridLayer = null;
  }
  if (stationLayer) {
    stationLayer.remove();
    stationLayer = null;
  }
  if (networkLayer) {
    networkLayer.remove();
    networkLayer = null;
  }

  gridLayer = L.geoJSON(data.geojson as unknown as GeoJSON.GeoJsonObject, {
    style: (f) => {
      const p = (f as GeoJSON.Feature).properties as
        | { level: number }
        | undefined;
      const lv = p?.level ?? 1;
      return {
        color: "#ffffff",
        weight: 0.4,
        fillOpacity: 0.55,
        fillColor: LEVEL_COLORS[lv - 1] ?? "#999",
      };
    },
    onEachFeature: (f, layer) => {
      const p = (f as GeoJSON.Feature).properties as
        | { time_min: number }
        | undefined;
      layer.bindTooltip(`理论可达约 ${p?.time_min ?? "?"} 分钟`);
    },
  }).addTo(map);

  stationLayer = L.layerGroup(
    data.entryStations.map((s) =>
      L.circleMarker([s.lat, s.lon], {
        radius: 5,
        color: s.type === "metro" ? "#7c3aed" : "#0f766e",
        fillColor: s.type === "metro" ? "#a78bfa" : "#2dd4bf",
        fillOpacity: 0.9,
      }).bindTooltip(`${s.name} · 步行约 ${s.walkMin.toFixed(1)} 分钟`),
    ),
  ).addTo(map);

  // 线路层最后添加,保持在网格与站点之上;网格移除时一并清掉
  updateNetworkLayer(data);
}
</script>

<template>
  <div class="layout">
    <aside class="panel">
      <h1>公共交通可达区域分析</h1>
      <p class="subtitle">
        基于公共交通网络拓扑的<strong>理论可达时间</strong>估算(非实时导航)
      </p>

      <section>
        <h2>1. 城市</h2>
        <select v-model="city" class="city-select">
          <option v-for="c in cities" :key="c.code" :value="c.code">
            {{ c.name }}
          </option>
        </select>
      </section>

      <section>
        <h2>2. 选择起点</h2>
        <div class="search-row">
          <input
            v-model="query"
            class="search"
            placeholder="输入地名搜索,如:广州塔"
            @keydown.enter="search"
          />
          <button
            class="search-btn"
            :disabled="searching || !query.trim()"
            @click="search"
          >
            {{ searching ? "…" : "搜索" }}
          </button>
        </div>
        <ul v-if="geoResults.length" class="geo-list">
          <li v-for="(r, i) in geoResults" :key="i" @click="chooseResult(r)">
            {{ r.name }}
          </li>
        </ul>
        <p v-else-if="searchDone && !searching" class="hint">
          无结果,请换个关键词
        </p>
        <p class="hint">也可直接在地图上单击选点</p>
        <div v-if="picked" class="picked">
          已选: {{ picked.lat.toFixed(5) }}, {{ picked.lon.toFixed(5) }}
        </div>
        <div v-else class="picked none">尚未选点</div>
      </section>

      <section>
        <h2>3. 最大出行时间</h2>
        <div class="times">
          <label
            v-for="t in [30, 45, 60, 90]"
            :key="t"
            :class="{ active: maxTime === t }"
          >
            <input type="radio" :value="t" v-model="maxTime" /> {{ t }} 分钟
          </label>
        </div>
      </section>

      <button class="go" :disabled="computing || !picked" @click="compute">
        {{ computing ? "计算中…" : "计算可达区域" }}
      </button>

      <p v-if="errorMsg" class="error">{{ errorMsg }}</p>

      <section v-if="result" class="stats">
        <h2>计算结果</h2>
        <p v-if="result.notice" class="warn">{{ result.notice }}</p>
        <p v-if="result.testData" class="warn">
          ⚠ 当前基于 TEST DATA(合成测试网络)
        </p>
        <ul>
          <li>
            可达站点:<strong>{{ result.reachableStations }}</strong>
          </li>
          <li>
            可达网格:<strong>{{ result.gridCells }}</strong> (500m×500m)
          </li>
          <li>
            计算耗时:<strong>{{ result.computeMs }}</strong> ms
          </li>
          <li>
            入口站点:<strong>{{ result.entryStations.length }}</strong> 个
          </li>
        </ul>
        <div class="legend">
          <span v-for="item in legend" :key="item.label"
            ><i :style="`background:${item.color}`"></i>{{ item.label }}</span
          >
        </div>
      </section>
    </aside>
    <main class="map-wrap">
      <div class="map-controls">
        <select v-model="basemap" class="basemap-select">
          <option v-for="(def, key) in BASEMAPS" :key="key" :value="key">
            {{ def.name }}
          </option>
        </select>
        <label class="network-toggle">
          <input type="checkbox" v-model="showNetwork" />
          交通线路
        </label>
      </div>
      <div id="map"></div>
    </main>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  height: 100%;
}
.panel {
  width: 340px;
  flex-shrink: 0;
  overflow-y: auto;
  padding: 20px;
  background: #f8fafc;
  border-right: 1px solid #e2e8f0;
}
h1 {
  font-size: 18px;
  margin-bottom: 6px;
}
.subtitle {
  font-size: 12px;
  color: #64748b;
  line-height: 1.6;
  margin-bottom: 16px;
}
h2 {
  font-size: 13px;
  margin: 14px 0 6px;
  color: #334155;
}
.hint {
  font-size: 12px;
  color: #94a3b8;
  margin: 6px 0;
}
.city-select {
  width: 100%;
  padding: 8px;
  font-size: 14px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: #fff;
}
.search-row {
  display: flex;
  gap: 6px;
}
.search {
  flex: 1;
  padding: 8px;
  font-size: 13px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
}
.search-btn {
  padding: 8px 14px;
  font-size: 13px;
  background: #0f766e;
  color: #fff;
  border: none;
  border-radius: 6px;
  cursor: pointer;
}
.search-btn:disabled {
  background: #94a3b8;
  cursor: not-allowed;
}
.geo-list {
  list-style: none;
  margin-top: 6px;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  overflow: hidden;
  max-height: 180px;
  overflow-y: auto;
}
.geo-list li {
  padding: 7px 10px;
  font-size: 12px;
  cursor: pointer;
  border-bottom: 1px solid #f1f5f9;
  line-height: 1.5;
}
.geo-list li:hover {
  background: #eff6ff;
}
.picked {
  font-size: 12px;
  padding: 6px 8px;
  background: #eff6ff;
  border-radius: 6px;
  color: #1d4ed8;
}
.picked.none {
  background: #f1f5f9;
  color: #94a3b8;
}
.times {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.times label {
  padding: 6px 12px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  background: #fff;
}
.times label.active {
  border-color: #2563eb;
  background: #dbeafe;
  color: #1d4ed8;
}
.times input {
  display: none;
}
.go {
  width: 100%;
  margin-top: 16px;
  padding: 10px;
  font-size: 14px;
  background: #2563eb;
  color: #fff;
  border: none;
  border-radius: 8px;
  cursor: pointer;
}
.go:disabled {
  background: #94a3b8;
  cursor: not-allowed;
}
.error {
  margin-top: 10px;
  padding: 8px;
  font-size: 12px;
  color: #b91c1c;
  background: #fee2e2;
  border-radius: 6px;
}
.warn {
  font-size: 12px;
  color: #b45309;
  background: #fef3c7;
  padding: 6px 8px;
  border-radius: 6px;
  margin-bottom: 8px;
}
.stats ul {
  list-style: none;
  font-size: 13px;
  line-height: 2;
  color: #334155;
}
.legend {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 10px;
  font-size: 12px;
}
.legend i {
  display: inline-block;
  width: 14px;
  height: 14px;
  margin-right: 6px;
  vertical-align: -2px;
  border-radius: 3px;
}
.map-wrap {
  flex: 1;
  position: relative;
}
#map {
  position: absolute;
  inset: 0;
}
.map-controls {
  position: absolute;
  top: 10px;
  right: 10px;
  z-index: 1000;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 6px;
}
.basemap-select {
  padding: 6px 10px;
  font-size: 13px;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.15);
  cursor: pointer;
}
.network-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  font-size: 13px;
  color: #334155;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  background: #fff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.15);
  cursor: pointer;
  user-select: none;
}
.network-toggle input {
  cursor: pointer;
}
</style>
