// 知华设备中心 HTTP 服务。
// 上海如静知华信息科技有限公司：https://www.zhuatech.cn/
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
package main

import (
	"cn.zhuatech/devicehub/internal/hub"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type application struct {
	s    *hub.Service
	file string
	key  string
	mu   sync.Mutex
}

func (a *application) persist() {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(a.file), 0755)
	b, _ := json.MarshalIndent(a.s.Snapshot(), "", "  ")
	_ = os.WriteFile(a.file, b, 0600)
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func read(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}
func main() {
	file := getenv("DEVICEHUB_DATA_FILE", "./data/devicehub.json")
	snap := hub.Snapshot{}
	if b, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(b, &snap)
	}
	a := &application{s: hub.NewService(snap), file: file, key: getenv("DEVICEHUB_API_KEY", "zhuatech-demo-key")}
	a.seed()
	a.persist()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "UP"}) })
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("/api/", a.auth(http.HandlerFunc(a.api)))
	mux.Handle("/", http.FileServer(http.Dir("web")))
	port := getenv("PORT", "18082")
	log.Printf("ZhuaTech DeviceHub running at http://127.0.0.1:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
func (a *application) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != a.key {
			write(w, 401, map[string]string{"error": "UNAUTHORIZED"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *application) api(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recover() != nil {
			write(w, 400, map[string]string{"error": "BAD_REQUEST"})
		}
	}()
	if r.Method == "GET" && r.URL.Path == "/api/dashboard" {
		write(w, 200, a.s.Dashboard())
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/devices" {
		var x struct{ Name, Product, Site, Token string }
		if read(r, &x) != nil {
			write(w, 400, map[string]string{"error": "INVALID_JSON"})
			return
		}
		d, secret, err := a.s.Enroll(x.Name, x.Product, x.Site, x.Token)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.persist()
		write(w, 201, map[string]any{"device": d, "secret": secret})
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/rollouts" {
		var x struct {
			Version   string
			Checksum  string
			DeviceIDs []string
			BatchSize int
		}
		if read(r, &x) != nil {
			write(w, 400, map[string]string{"error": "INVALID_JSON"})
			return
		}
		rollout, err := a.s.CreateRollout(x.Version, x.Checksum, x.DeviceIDs, x.BatchSize)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.persist()
		write(w, 201, rollout)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "rollouts" && parts[3] == "next" && r.Method == "POST" {
		batch, err := a.s.NextRolloutBatch(parts[2])
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.persist()
		write(w, 200, map[string]any{"deviceIds": batch})
		return
	}
	if len(parts) == 4 && parts[0] == "api" && parts[1] == "commands" && parts[3] == "complete" && r.Method == "POST" {
		var x struct{ Success bool }
		if read(r, &x) != nil {
			write(w, 400, map[string]string{"error": "INVALID_JSON"})
			return
		}
		command, err := a.s.CompleteCommand(parts[2], x.Success)
		if err != nil {
			write(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.persist()
		write(w, 200, command)
		return
	}
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "devices" {
		id := parts[2]
		if r.Method == "POST" && len(parts) == 4 && parts[3] == "heartbeat" {
			var x struct {
				Firmware string
				Reported map[string]any
			}
			_ = read(r, &x)
			v, err := a.s.Heartbeat(id, x.Firmware, x.Reported)
			if err != nil {
				write(w, 400, map[string]string{"error": err.Error()})
				return
			}
			a.persist()
			write(w, 200, map[string]any{"desiredDelta": v})
			return
		}
		if r.Method == "PUT" && len(parts) == 4 && parts[3] == "desired" {
			var x map[string]any
			_ = read(r, &x)
			v, err := a.s.UpdateDesired(id, x)
			if err != nil {
				write(w, 400, map[string]string{"error": err.Error()})
				return
			}
			a.persist()
			write(w, 200, v)
			return
		}
		if r.Method == "POST" && len(parts) == 4 && parts[3] == "commands" {
			var x struct {
				Name, IdempotencyKey string
				Payload              map[string]any
			}
			_ = read(r, &x)
			v, err := a.s.IssueCommand(id, x.Name, x.IdempotencyKey, x.Payload)
			if err != nil {
				write(w, 400, map[string]string{"error": err.Error()})
				return
			}
			a.persist()
			write(w, 201, v)
			return
		}
		if r.Method == "POST" && len(parts) == 4 && parts[3] == "telemetry" {
			var x struct {
				Metric      string
				Value, High float64
			}
			_ = read(r, &x)
			v, err := a.s.IngestTelemetry(id, x.Metric, x.Value, x.High)
			if err != nil {
				write(w, 400, map[string]string{"error": err.Error()})
				return
			}
			a.persist()
			write(w, 202, map[string]any{"alert": v})
			return
		}
	}
	write(w, 404, map[string]string{"error": "NOT_FOUND"})
}
func (a *application) seed() {
	if len(a.s.Snapshot().Devices) > 0 {
		return
	}
	d, _, _ := a.s.Enroll("冷链温湿度网关", "ZT-EDGE-T100", "上海一号仓", "demo-token")
	_, _ = a.s.UpdateDesired(d.ID, map[string]any{"sampleSeconds": 30, "reportSeconds": 120})
	_, _ = a.s.Heartbeat(d.ID, "1.4.2", map[string]any{"sampleSeconds": 60, "signal": -57})
	_, _ = a.s.IngestTelemetry(d.ID, "temperature", 9.4, 8)
}
func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
