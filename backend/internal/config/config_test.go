package config

import (
	"os"
	"testing"
)

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	p := t.TempDir() + "/config.json"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTmp(t, `{
		"server": {"port": 8080},
		"data": {"fallback": "../data/test"},
		"params": {"walkingSpeedKmh":5,"busSpeedKmh":20,"metroSpeedKmh":35,"transferPenaltyMin":5,"maxWalkingDistanceM":1500,"entryStationsLimit":6},
		"grid": {"cellSizeM":500,"walkRadiusM":800,"levels":[30,45,60]},
		"cities": [
			{"code":"guangzhou","name":"广州","bbox":[112.9,22.45,114.05,23.95],"dataDir":"../data/guangzhou"},
			{"code":"foshan","name":"佛山","bbox":[112.8,22.8,113.4,23.4],"dataDir":"../data/foshan"}
		]
	}`)
	c, err := Load(p)
	if err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
	if len(c.Cities) != 2 || c.Cities[1].Name != "佛山" {
		t.Fatal("城市配置读取错误")
	}
	if c.Params.BusSpeedKmh != 20 || c.Grid.CellSizeM != 500 {
		t.Fatal("配置值读取错误")
	}
}

func TestLoadInvalidSpeed(t *testing.T) {
	p := writeTmp(t, `{
		"params": {"walkingSpeedKmh":0,"busSpeedKmh":20,"metroSpeedKmh":35,"transferPenaltyMin":5,"maxWalkingDistanceM":1500,"entryStationsLimit":6},
		"grid": {"cellSizeM":500,"walkRadiusM":800,"levels":[30]},
		"cities": [{"code":"gz","name":"广州","bbox":[113,22,114,23],"dataDir":"d"}]
	}`)
	if _, err := Load(p); err == nil {
		t.Fatal("步行速度为 0 应报错")
	}
}

func TestLoadInvalidCities(t *testing.T) {
	cases := []struct{ name, cities string }{
		{"无城市", `"cities": []`},
		{"bbox反序", `"cities": [{"code":"gz","name":"广州","bbox":[114,22,113,23],"dataDir":"d"}]`},
		{"code重复", `"cities": [{"code":"gz","name":"A","bbox":[113,22,114,23],"dataDir":"d"},{"code":"gz","name":"B","bbox":[113,22,114,23],"dataDir":"d2"}]`},
		{"缺dataDir", `"cities": [{"code":"gz","name":"广州","bbox":[113,22,114,23]}]`},
	}
	for _, tc := range cases {
		p := writeTmp(t, `{
			"params": {"walkingSpeedKmh":5,"busSpeedKmh":20,"metroSpeedKmh":35,"transferPenaltyMin":5,"maxWalkingDistanceM":1500,"entryStationsLimit":6},
			"grid": {"cellSizeM":500,"walkRadiusM":800,"levels":[30]},`+tc.cities+`}`)
		if _, err := Load(p); err == nil {
			t.Fatalf("%s 应报错", tc.name)
		}
	}
}
