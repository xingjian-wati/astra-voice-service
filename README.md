```
astra-voice-service/
├── cmd/
│   └── server/                    # 应用入口
│       └── main.go
│
├── internal/                      # 私有应用代码（不对外暴露）
│   │
│   ├── adapters/                  # Protocol Adapter Layer（协议适配层）
│   │   ├── webrtc/                 # WebRTC 协议适配器
│   │   │   ├── client.go          # OpenAI WebRTC 客户端（原 openai/webrtc_client.go）
│   │   │   ├── processor.go       # WhatsApp WebRTC 处理器（原 webrtc_processor.go）
│   │   │   └── template.go        # WebRTC 模板（原 pion_webrtc_template.go）
│   │   ├── http/                   # HTTP 协议适配器
│   │   │   └── wati_client.go     # Wati API 客户端
│   │   └── livekit/                # LiveKit 协议适配器
│   │       ├── room_manager.go
│   │       ├── audio_processor.go
│   │       └── opus_writer.go
│   │
│   ├── core/                       # Core Logic（核心业务逻辑层）
│   │   ├── event/                  # Event Manager
│   │   │   ├── bus.go              # 事件总线（原 eventbus/bus.go）
│   │   │   ├── events.go
│   │   │   └── lifecycle.go
│   │   ├── tool/                   # Tool Executor
│   │   │   └── manager.go          # 工具管理器（原 tools/tool_manager.go）
│   │   ├── model/                  # Model Router
│   │   │   └── router.go           # OpenAI Handler（原 openai/handler.go）
│   │   └── context/                # Context Manager
│   │       ├── connection.go       # 连接管理（原 openai/connection.go）
│   │       ├── session.go          # 会话管理（原 openai/session.go）
│   │       ├── events.go           # 事件处理（原 openai/events.go）
│   │       └── messaging.go        # 消息处理（原 openai/messaging.go）
│   │
│   ├── services/                   # 业务服务层
│   │   ├── agent/                  # Agent Service
│   │   │   └── service.go          # 代理服务（原 agent_service.go）
│   │   ├── call/                   # Call Service
│   │   │   ├── service.go          # WhatsApp 呼叫服务（原 whatsapp_service.go）
│   │   │   └── structs.go          # 呼叫相关结构（原 whatsapp_structs.go）
│   │   └── conversation/           # Conversation Service
│   │       └── service.go
│   │
│   ├── domain/                     # 领域模型（Domain Layer）
│   │   ├── agent.go
│   │   ├── tenant.go
│   │   ├── conversation.go
│   │   └── common.go
│   │
│   ├── repository/                 # 数据访问层（Repository Layer）
│   │   ├── db.go
│   │   ├── connection.go
│   │   ├── api_connection.go
│   │   ├── dify_connection.go
│   │   ├── voice_agent_repo.go
│   │   ├── voice_tenant_repo.go
│   │   ├── voice_conversation_repo.go
│   │   └── dify_api_token_repo.go
│   │
│   ├── handler/                    # HTTP 处理层（Presentation Layer）
│   │   ├── routes.go
│   │   ├── middleware.go
│   │   ├── agent_handler.go
│   │   ├── tenant_handler.go
│   │   ├── voice_conversation_handler.go
│   │   ├── wati_webhook_handler.go
│   │   ├── livekit_handler.go
│   │   ├── livekit_webhook_handler.go
│   │   ├── openai_handler.go
│   │   ├── outbound_webhook_handler.go
    │   │   ├── webrtc_config_handler.go
    │   │   ├── static_handler.go
    │   │   └── brandkit_helpers.go
│   │
│   ├── config/                     # 应用配置（Application Config）
│   │   ├── app.go                  # 应用配置（原 config/config.go）
│   │   ├── websocket.go            # WebSocket 配置（原 config/websocket.go）
│   │   ├── api.go                  # API 配置（原 config/api.go）
│   │   ├── mcp.go                  # MCP 配置（原 config/mcp.go）
│   │   ├── rag.go                  # RAG 配置（原 config/rag.go）
│   │   ├── whatsapp.go             # WhatsApp 配置（原 whatsappcall/config/whatsapp_config.go）
│   │   ├── agent.go                # Agent 配置（原 whatsappcall/config/agent_config.go）
│   │   ├── agent_fetcher.go        # Agent 获取器（原 agent_fetcher_factory.go等）
│   │   └── language_accent.go      # 语言口音配置
│   │
│   ├── prompts/                    # Prompt 生成
│   │   └── generator.go            # Prompt 生成器（原 prompts/agent_prompt_generator.go）
│   │
│   └── storage/                     # 存储层
│       ├── audio.go
│       ├── ogg.go
│       └── pdf.go
│
├── pkg/                            # 可被外部使用的库代码
│   │
│   ├── data/                       # Data Mgr（数据管理层）
│   │   ├── mapping/                # Mapping Service
│   │   │   └── service.go          # 映射服务（原 pkg/mapping_service.go）
│   │   ├── mcp/                    # MCP Service
│   │   │   └── service.go          # Composio/MCP 服务（原 pkg/composio_service.go）
│   │   └── api/                    # API Service
│   │       └── service.go          # API 服务（原 pkg/api_service.go）
│   │
│   ├── logger/                     # 日志工具（可复用）
│   │   └── logger.go
│   │
│   ├── pubsub/                     # PubSub 服务
│   │   └── service.go              # PubSub 服务（原 pkg/pubsub_service.go）
│   │
│   ├── redis/                      # Redis 服务
│   │   └── service.go              # Redis 服务（原 pkg/redis_service.go）
│   │
│   ├── usage/                      # Usage 服务
│   │   └── service.go              # Usage 服务（原 pkg/usage_service.go）
│   │
│   ├── twilio/                     # Twilio 服务
│   │   └── token_service.go        # Twilio Token 服务
│   │
│   ├── gcs/                        # GCS 服务
│   │   └── service.go              # GCS 服务（原 pkg/gcs.go）
│   │
│   └── rag/                        # RAG 客户端（可复用）
│       ├── client.go               # RAG 客户端（原 rag/rag_client.go）
│       ├── agent_rag.go            # Agent RAG（原 rag/agent_rag.go）
│       └── translator.go           # 翻译器（原 rag/translator.go）
│
├── api/                            # API 定义（可选）
│   └── openapi/                    # OpenAPI 定义
│
├── scripts/                        # 脚本工具
│   ├── migrate_logging.py
│   ├── init-db-updated.sql
│   ├── init-api-db.sql
│   └── ...
│
├── static/                         # 静态资源
│   ├── html/
│   ├── css/
│   ├── js/
│   └── images/
│
├── docker-compose.dev.yml          # 开发环境配置
├── docker-compose.prod.yml         # 生产环境配置
├── Dockerfile.whatsapp-call         # Docker 构建文件
├── go.mod
├── go.sum
└── README.md


结构说明
1. internal/adapters/ - Protocol Adapter Layer
协议适配器，处理外部协议（WebRTC、HTTP、LiveKit）
与业务逻辑解耦
2. internal/core/ - Core Logic
Event Manager: 事件管理
Tool Executor: 工具执行
Model Router: 模型路由（OpenAI Handler）
Context Manager: 上下文管理
3. internal/services/ - 业务服务层
业务逻辑服务（Agent、Call、Conversation）
4. pkg/data/ - Data Mgr
Mapping Service、MCP Service、API Service
可被外部使用
5. internal/config/ - 应用配置
应用级配置，不对外暴露
6. internal/repository/ - 数据访问层
数据库访问，实现领域模型持久化
7. internal/domain/ - 领域模型
核心业务实体
8. internal/handler/ - HTTP 处理层
HTTP 路由和处理
关键原则
internal/：应用私有代码，不对外暴露
pkg/：可复用的库代码
分层清晰：Adapter → Core → Service → Repository
职责单一：每个目录职责明确

```
