# Kairos Quickstart

[简体中文](README.zh-CN.md)

This example demonstrates two Tasks that are immediately available in parallel and a third Task that opens only after both upstream results are submitted. Every executor uses the same durable WorkItem, while an exclusive Claim prevents duplicate execution.

## Run the example

With a release package that includes this example, only curl and the packaged server are needed to start it. From the extracted directory:

```sh
sh examples/quickstart/run.sh
```

The older `v0.1.0-alpha.1` archive does not include quickstart. Follow its [manual installation path](../../docs/installation.md) instead.

From source, prerequisites are Go 1.26.10 or later, Node.js 22.22.2+ (22.x), 24.15.0+ (24.x), or 26+, npm, and curl.

From the repository root:

```bash
make quickstart
```

`make quickstart` builds Kairos first; the packaged command uses its prebuilt binary. Both start at `http://127.0.0.1:8080` in Trusted Mode and seed an isolated example Workflow and WorkItem, ignoring an inherited PostgreSQL DSN. Open the URL to follow execution in the operations console. The temporary database and Artifact directory are removed when you press Ctrl-C.

If port 8080 is occupied, choose another address:

```bash
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 make quickstart
# In an extracted package:
KAIROS_QUICKSTART_ADDR=127.0.0.1:8081 sh examples/quickstart/run.sh
```

## Connect agents

The checkout and new packages contain `.codex/config.toml` and `.agents/skills/kairos-agent`. Use the Codex command printed by the script, which includes the actual MCP URL. For the default port, from separate terminals in that directory:

```bash
KAIROS_ACTOR_ID=quickstart-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex -c 'mcp_servers.kairos.url="http://127.0.0.1:8080/mcp"'
```

Trust the directory in Codex so it loads the project MCP configuration. If you changed the listening port, use the printed URL instead of 8080. To run Agents in a different project, follow [standalone Agent setup](../../docs/installation.md#connect-your-agent); installing the Skill alone does not configure MCP. Codex CLI login is required for actual Agent execution.

Use `quickstart-agent-2` for the second session. Ask each session:

```text
Use $kairos-agent to find and complete one available Task.
```

Two sessions can Claim the two initial Tasks concurrently. A third attempt will find no eligible Task until one becomes available. After both initial results are submitted, Kairos opens the join Task with both upstream results in its execution context.

Running a single session also works; it can complete the Tasks sequentially.

`workflow.json` sets `max_task_instances_per_node` to 10 per Definition node. Each node counts independently; the Workflow has no total Task-instance limit.

To try recovery, fail one branch and use **Continue execution** in the console; its replacement joins the successful branch. See [Workflow recovery](../../docs/api-reference.md#workflow-recovery) for recovery and Start over.
