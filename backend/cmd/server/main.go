// 公共交通可达区域分析系统 后端入口。
// 启动时把所有已配置城市的图数据加载进内存,提供 REST API。
package main

import (
	"flag"
	"fmt"
	"log"

	"backend/internal/api"
	"backend/internal/config"
	"backend/internal/service"
)

func main() {
	cfgPath := flag.String("config", "config/config.json", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 逐城市加载;全部失败才回退 TEST DATA
	engines := make(map[string]*service.Engine, len(cfg.Cities))
	var failed []string
	for _, ct := range cfg.Cities {
		eng, err := service.LoadCity(cfg, ct)
		if err != nil {
			log.Printf("[warn] 跳过城市 %s: %v", ct.Name, err)
			failed = append(failed, ct.Code)
			continue
		}
		engines[ct.Code] = eng
		fmt.Printf("[city] %-8s %d 节点 / %d 弧,加载 %dms,目录 %s\n",
			ct.Code, eng.Graph.NodeCount(), eng.Graph.ArcCount(), eng.LoadMs, ct.DataDir)
	}
	if len(engines) == 0 {
		log.Printf("⚠⚠⚠  所有城市数据均缺失,回退 TEST DATA(合成测试网络)!")
		scope := cfg.Cities[0]
		eng, err := service.LoadFallback(cfg, scope)
		if err != nil {
			log.Fatalf("回退数据也不可用: %v", err)
		}
		engines[scope.Code] = eng
	}

	r := api.NewRouter(cfg, engines)
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Server.Port)
	fmt.Printf("后端启动: http://%s  (GET /api/health,城市数 %d)\n", addr, len(engines))
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
