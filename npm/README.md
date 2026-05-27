# Universe — AI Token Optimization for Cursor

Universe gives your Cursor IDE 5 AI engines that work together to dramatically reduce token usage while improving code quality:

1. **Knowledge Graph** — maps your codebase; Claude only sees relevant context
2. **Persistent Memory** — stores observations across sessions
3. **Self-Evolving Skills** — reusable solutions that improve over time
4. **Compression** — strips tokens using context that Claude already has
5. **Plan-Bridge** — splits planning (premium model) from execution (cheap model)

## Quick start

```bash
# Install
npm install -g @atlas/universe

# Scan your project
cd my-project
universe init

# Configure models and generate Cursor workspace files
universe setup
# Pick your premium model (Opus, GPT-4o, Gemini Pro, etc.)
# Pick your execution model (Haiku, GPT-4o-mini, Flash, etc.)

# Open both Cursor windows
universe start
# 🧠 Planner window (premium) + ⚡ Executor window (cheap)

# Workflow:
#   In 🧠 Planner: "Fix the type mismatch in auth.ValidateToken"
#   In ⚡ Executor: "execute"
#   In 🧠 Planner: "verify"
```

## Connect database (optional — enables memory and skills)

```bash
docker compose up -d
universe config set db postgres://universe_admin:universe_secret_2024@localhost:5433/universe
universe db migrate
```

## Commands

| Command | What it does |
|---------|-------------|
| `universe init` | Scan codebase and build knowledge graph |
| `universe setup` | Interactive setup — pick models, generate config files |
| `universe plan` | Open planner Cursor window (premium model) |
| `universe exec` | Open executor Cursor window (execution model) |
| `universe start` | Open both windows |
| `universe status` | Show all 5 engine statuses + model config |
| `universe dashboard` | Open the dashboard (port 3001) |
| `universe config set db <url>` | Connect to PostgreSQL |
| `universe config set premium_model <name>` | Change premium model |
| `universe config set execution_model <name>` | Change execution model |
| `universe config get db` | Show database connection |
| `universe config get models` | Show model configuration |
| `universe db status` | Test database connection |
| `universe db migrate` | Run database migrations |
| `universe skills list` | List all active skills |
| `universe setup-rules` | Regenerate Cursor rules |
| `universe mcp --repo .` | Run MCP server (for Cursor connection) |

## MCP setup (Cursor)

After `universe setup`, your `.cursor/mcp.json` is automatically configured. Restart Cursor and the Universe MCP server will appear in the tools list.

## License

MIT
