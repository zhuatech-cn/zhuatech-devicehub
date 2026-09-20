# 架构与适配边界

控制面由 Go 领域服务、HTTP API、持久化端口和运营界面组成。设备数据面通过 HTTP 示例协议进行心跳、影子与遥测同步；MQTT、CoAP、LwM2M 可作为独立传输适配器接入。

企业化替换点：JSON → MySQL/PostgreSQL；内存遥测 → TDengine/InfluxDB；HTTP 设备通道 → MQTT；单实例锁 → 分布式租约。设备身份、命令幂等、影子差异和固件批次状态机保持不变。
