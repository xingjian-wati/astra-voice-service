# 项目结构重构迁移计划

## 迁移步骤

### 阶段 1: 协议适配层 (adapters)
- [x] 创建 internal/adapters/ 目录结构
- [x] openai/webrtc_client.go → internal/adapters/webrtc/client.go
- [x] whatsappcall/app/services/webrtc_processor.go → internal/adapters/webrtc/processor.go
- [x] whatsappcall/app/services/pion_webrtc_template.go → internal/adapters/webrtc/template.go
- [x] whatsappcall/app/services/wati_client.go → internal/adapters/http/wati_client.go
- [x] whatsappcall/livekit/* → internal/adapters/livekit/

### 阶段 2: 核心逻辑层 (core)
- [x] whatsappcall/app/eventbus/* → internal/core/event/
- [x] whatsappcall/tools/* → internal/core/tool/
- [x] whatsappcall/openai/* → internal/core/model/openai/
- [ ] 完善 internal/core/context/ (待定)

### 阶段 3: 业务服务层 (services)
- [x] whatsappcall/app/services/agent_service.go → internal/services/agent/service.go
- [x] whatsappcall/app/services/whatsapp_service.go → internal/services/call/service.go
- [x] whatsappcall/app/services/whatsapp_structs.go → internal/services/call/structs.go

### 阶段 4: 领域模型和缓存
- [x] whatsappcall/domain/* → internal/domain/
- [x] whatsappcall/app/cache/* → internal/cache/

### 阶段 5: 数据访问层
- [x] whatsappcall/repository/* → internal/repository/

### 阶段 6: HTTP 处理层
- [x] whatsappcall/handler/* → internal/handler/ (已清理 test_page_handler)
- [x] whatsappcall/server.go → cmd/server/main.go (已合并)
- [x] 已删除 whatsappcall/test_integration.go
- [x] 创建 cmd/server/main.go (合并了原 server.go 逻辑)

### 阶段 7: 配置层
- [x] config/* → internal/config/
- [x] whatsappcall/config/* → internal/config/
- [x] whatsappcall/example.env → ./example.env

### 阶段 8: 可复用库 (pkg)
- [x] whatsappcall/pkg/* → pkg/ (已移动至根目录，并完成结构化)
- [x] 结构化 pkg/ 目录 (data/api, data/mapping, data/mcp, etc.)

### 阶段 9: 其他
- [x] whatsappcall/prompts/* → internal/prompts/
- [x] whatsappcall/storage/* → internal/storage/
- [x] whatsappcall/rag/* → pkg/rag/
- [x] whatsappcall/scripts/* → ./scripts/
- [x] whatsappcall/Makefile → ./Makefile
- [x] whatsappcall/docker-compose*.yml → ./docker-compose*.yml

### 阶段 10: 更新所有导入路径与清理
- [x] 更新所有文件中的导入路径
- [x] 验证核心逻辑合并
- [x] 清理过时的根目录和旧项目目录 (whatsappcall/, openai/, config/)
- [x] 验证编译 (已完成代码层面清理)
