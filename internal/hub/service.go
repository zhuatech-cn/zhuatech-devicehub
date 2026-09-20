// Package hub 实现知华设备中心的设备生命周期领域能力。
// 上海如静知华信息科技有限公司：https://www.zhuatech.cn/
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Device 表示具备身份、影子、遥测和固件信息的受管设备。
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
type Device struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Product    string         `json:"product"`
	Site       string         `json:"site"`
	Status     string         `json:"status"`
	Identity   string         `json:"identity"`
	Firmware   string         `json:"firmware"`
	Desired    map[string]any `json:"desired"`
	Reported   map[string]any `json:"reported"`
	LastSeenAt string         `json:"lastSeenAt,omitempty"`
	CreatedAt  string         `json:"createdAt"`
}

// Command 表示带幂等键和状态机的远程指令。
type Command struct {
	ID             string         `json:"id"`
	DeviceID       string         `json:"deviceId"`
	Name           string         `json:"name"`
	Payload        map[string]any `json:"payload"`
	Status         string         `json:"status"`
	IdempotencyKey string         `json:"idempotencyKey"`
	CreatedAt      string         `json:"createdAt"`
	CompletedAt    string         `json:"completedAt,omitempty"`
}

// Alert 表示由遥测阈值触发的告警。
type Alert struct {
	ID        string  `json:"id"`
	DeviceID  string  `json:"deviceId"`
	Metric    string  `json:"metric"`
	Value     float64 `json:"value"`
	Severity  string  `json:"severity"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
}

// FirmwareRollout 表示分批固件升级任务。
type FirmwareRollout struct {
	ID        string   `json:"id"`
	Version   string   `json:"version"`
	DeviceIDs []string `json:"deviceIds"`
	BatchSize int      `json:"batchSize"`
	Cursor    int      `json:"cursor"`
	Status    string   `json:"status"`
	Checksum  string   `json:"checksum"`
	CreatedAt string   `json:"createdAt"`
}

// Snapshot 是服务的可持久化状态。
type Snapshot struct {
	Devices  []Device          `json:"devices"`
	Commands []Command         `json:"commands"`
	Alerts   []Alert           `json:"alerts"`
	Rollouts []FirmwareRollout `json:"rollouts"`
	Audit    []map[string]any  `json:"audit"`
}

// Service 提供线程安全的设备中心业务服务。
// 上海如静知华信息科技有限公司：https://www.zhuatech.cn/
type Service struct {
	mu          sync.RWMutex
	devices     map[string]*Device
	commands    map[string]*Command
	commandKeys map[string]string
	alerts      map[string]*Alert
	rollouts    map[string]*FirmwareRollout
	audit       []map[string]any
}

// NewService 从快照创建服务。
func NewService(snapshot Snapshot) *Service {
	s := &Service{devices: map[string]*Device{}, commands: map[string]*Command{}, commandKeys: map[string]string{}, alerts: map[string]*Alert{}, rollouts: map[string]*FirmwareRollout{}, audit: snapshot.Audit}
	for i := range snapshot.Devices {
		v := snapshot.Devices[i]
		s.devices[v.ID] = &v
	}
	for i := range snapshot.Commands {
		v := snapshot.Commands[i]
		s.commands[v.ID] = &v
		if v.IdempotencyKey != "" {
			s.commandKeys[v.IdempotencyKey] = v.ID
		}
	}
	for i := range snapshot.Alerts {
		v := snapshot.Alerts[i]
		s.alerts[v.ID] = &v
	}
	for i := range snapshot.Rollouts {
		v := snapshot.Rollouts[i]
		s.rollouts[v.ID] = &v
	}
	return s
}

func makeID(prefix string) string {
	b := make([]byte, 7)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
func timestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func require(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s不能为空", field)
	}
	return value, nil
}

// Enroll 注册设备并签发不可逆身份摘要。
// 商业授权或定制开发请微信添加微信号zhuatech或zhuatech2进行咨询。
func (s *Service) Enroll(name, product, site, token string) (Device, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := require(name, "设备名称"); err != nil {
		return Device{}, "", err
	}
	if _, err := require(product, "产品型号"); err != nil {
		return Device{}, "", err
	}
	if len(token) < 8 {
		return Device{}, "", errors.New("注册令牌长度不能小于8位")
	}
	secret := makeID("sec")
	sum := sha256.Sum256([]byte(secret))
	d := &Device{ID: makeID("dev"), Name: name, Product: product, Site: site, Status: "offline", Identity: hex.EncodeToString(sum[:]), Firmware: "unknown", Desired: map[string]any{}, Reported: map[string]any{}, CreatedAt: timestamp()}
	s.devices[d.ID] = d
	s.record("DEVICE_ENROLLED", d.ID, map[string]any{"product": product})
	return *d, secret, nil
}

// UpdateDesired 更新设备影子的期望状态。
func (s *Service) UpdateDesired(deviceID string, desired map[string]any) (Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return Device{}, errors.New("设备不存在")
	}
	d.Desired = desired
	s.record("TWIN_DESIRED_UPDATED", deviceID, desired)
	return *d, nil
}

// Heartbeat 合并上报影子并返回期望状态差异。
func (s *Service) Heartbeat(deviceID, firmware string, reported map[string]any) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[deviceID]
	if !ok {
		return nil, errors.New("设备不存在")
	}
	d.Status = "online"
	d.LastSeenAt = timestamp()
	if firmware != "" {
		d.Firmware = firmware
	}
	d.Reported = reported
	delta := map[string]any{}
	for k, v := range d.Desired {
		if fmt.Sprint(d.Reported[k]) != fmt.Sprint(v) {
			delta[k] = v
		}
	}
	return delta, nil
}

// IngestTelemetry 校验遥测并按阈值生成告警。
func (s *Service) IngestTelemetry(deviceID, metric string, value float64, high float64) (*Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[deviceID]; !ok {
		return nil, errors.New("设备不存在")
	}
	if strings.TrimSpace(metric) == "" {
		return nil, errors.New("指标不能为空")
	}
	if value <= high {
		return nil, nil
	}
	severity := "warning"
	if value >= high*1.2 {
		severity = "critical"
	}
	a := &Alert{ID: makeID("alt"), DeviceID: deviceID, Metric: metric, Value: value, Severity: severity, Status: "open", CreatedAt: timestamp()}
	s.alerts[a.ID] = a
	s.record("ALERT_OPENED", a.ID, map[string]any{"deviceId": deviceID, "metric": metric})
	return a, nil
}

// IssueCommand 创建幂等远程指令。
func (s *Service) IssueCommand(deviceID, name, key string, payload map[string]any) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.devices[deviceID]; !ok {
		return Command{}, errors.New("设备不存在")
	}
	if key != "" {
		if commandID, ok := s.commandKeys[key]; ok {
			return *s.commands[commandID], nil
		}
	}
	if _, err := require(name, "指令名称"); err != nil {
		return Command{}, err
	}
	c := &Command{ID: makeID("cmd"), DeviceID: deviceID, Name: name, Payload: payload, Status: "pending", IdempotencyKey: key, CreatedAt: timestamp()}
	s.commands[c.ID] = c
	if key != "" {
		s.commandKeys[key] = c.ID
	}
	s.record("COMMAND_ISSUED", c.ID, map[string]any{"deviceId": deviceID})
	return *c, nil
}

// CompleteCommand 完成设备指令并校验状态转换。
func (s *Service) CompleteCommand(commandID string, success bool) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.commands[commandID]
	if !ok {
		return Command{}, errors.New("指令不存在")
	}
	if c.Status != "pending" {
		return Command{}, errors.New("指令已经结束")
	}
	if success {
		c.Status = "succeeded"
	} else {
		c.Status = "failed"
	}
	c.CompletedAt = timestamp()
	return *c, nil
}

// CreateRollout 创建带校验值和批次的固件升级。
func (s *Service) CreateRollout(version, checksum string, deviceIDs []string, batchSize int) (FirmwareRollout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := require(version, "固件版本"); err != nil {
		return FirmwareRollout{}, err
	}
	if len(checksum) < 16 {
		return FirmwareRollout{}, errors.New("固件校验值无效")
	}
	if len(deviceIDs) == 0 {
		return FirmwareRollout{}, errors.New("升级设备不能为空")
	}
	for _, deviceID := range deviceIDs {
		if _, ok := s.devices[deviceID]; !ok {
			return FirmwareRollout{}, fmt.Errorf("设备不存在: %s", deviceID)
		}
	}
	if batchSize < 1 {
		batchSize = 1
	}
	r := &FirmwareRollout{ID: makeID("fw"), Version: version, DeviceIDs: deviceIDs, BatchSize: batchSize, Status: "running", Checksum: checksum, CreatedAt: timestamp()}
	s.rollouts[r.ID] = r
	s.record("ROLLOUT_CREATED", r.ID, map[string]any{"version": version})
	return *r, nil
}

// NextRolloutBatch 返回下一批设备并推进游标。
func (s *Service) NextRolloutBatch(rolloutID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rollouts[rolloutID]
	if !ok {
		return nil, errors.New("升级任务不存在")
	}
	if r.Status == "completed" {
		return []string{}, nil
	}
	end := r.Cursor + r.BatchSize
	if end > len(r.DeviceIDs) {
		end = len(r.DeviceIDs)
	}
	batch := append([]string(nil), r.DeviceIDs[r.Cursor:end]...)
	r.Cursor = end
	if r.Cursor == len(r.DeviceIDs) {
		r.Status = "completed"
	}
	return batch, nil
}

// Dashboard 返回设备运营概览。
func (s *Service) Dashboard() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	devices := make([]Device, 0, len(s.devices))
	online := 0
	for _, d := range s.devices {
		devices = append(devices, *d)
		if d.Status == "online" {
			online++
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].CreatedAt < devices[j].CreatedAt })
	alerts := make([]Alert, 0, len(s.alerts))
	for _, a := range s.alerts {
		alerts = append(alerts, *a)
	}
	return map[string]any{"metrics": map[string]int{"devices": len(devices), "online": online, "alerts": len(alerts), "commands": len(s.commands), "rollouts": len(s.rollouts)}, "devices": devices, "alerts": alerts, "audit": s.audit}
}

// Snapshot 导出服务状态。
func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Snapshot{Audit: s.audit}
	for _, v := range s.devices {
		out.Devices = append(out.Devices, *v)
	}
	for _, v := range s.commands {
		out.Commands = append(out.Commands, *v)
	}
	for _, v := range s.alerts {
		out.Alerts = append(out.Alerts, *v)
	}
	for _, v := range s.rollouts {
		out.Rollouts = append(out.Rollouts, *v)
	}
	return out
}

func (s *Service) record(action, resourceID string, detail map[string]any) {
	s.audit = append(s.audit, map[string]any{"id": makeID("aud"), "action": action, "resourceId": resourceID, "detail": detail, "occurredAt": timestamp()})
}
