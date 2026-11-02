# Install a release package

[简体中文](installation.zh-CN.md)

Download the archive for your OS (`darwin`/`linux`) and CPU (`arm64`/`amd64`) from [GitHub Releases](https://github.com/ScienJus/kairos/releases), together with `checksums.txt`. For an unreleased build, use the [release checklist](releasing.md).

Each package contains Core, Daemon, the console and examples; running it needs no Go or Node.js. Compare `shasum -a 256 archive.tar.gz` (macOS) or `sha256sum archive.tar.gz` (Linux) with the exact filename in `checksums.txt`. Published workflow assets also support `gh attestation verify archive.tar.gz --repo ScienJus/kairos`; local snapshots have no GitHub attestation.

Extract into a new directory and check both versions match the download:

```sh
tar -xzf archive.tar.gz
./kairos-server --version
./kairos-daemon --version
```

## Try the bundled example

Packages built from this revision include the quickstart, Skill and local Codex MCP configuration. From the extracted directory, run:

```sh
sh examples/quickstart/run.sh
```

This needs curl, starts Core on loopback, and creates two parallel Tasks followed by a join. No Go, Node.js or model call is needed to start the example. Connect Codex using the command printed by the script; follow the packaged [quickstart guide](../examples/quickstart/README.md) for the two sessions. Ctrl-C stops Core and removes the temporary example data.

The already published `v0.1.0-alpha.1` archive has no bundled quickstart, Skill or MCP configuration. With that version, use manual startup below, create a collaboration plan in the console, and follow [Connect your agent](#connect-your-agent). `make quickstart` is for a source checkout and is not a release-package command.

## Start Core with persistent local data

Stop any example using port 8080 first. From the extracted directory:

```sh
mkdir -m 700 data
KAIROS_AUTH_MODE=trusted KAIROS_POSTGRES_DSN='' \
KAIROS_LISTEN_ADDR=127.0.0.1:8080 \
KAIROS_SQLITE_PATH="$PWD/data/kairos.db" \
KAIROS_ARTIFACT_DIR="$PWD/data/artifacts" \
  ./kairos-server
```

Open `http://127.0.0.1:8080` for the console; `/healthz` checks health. Ctrl-C stops Core. Reuse the same data paths on restart without repeating `mkdir`. This local trial uses Trusted Mode; shared use requires [Authenticated Mode configuration](https://github.com/ScienJus/kairos/blob/main/docs/api-reference.md).

In an empty console, choose **Create work**. The first-use guide leads to a **Workflow** for known steps or the **Blackboard library** for work that agents plan as they go. Publish a Definition, then select **Use this Workflow/Blackboard** to create a WorkItem. On older consoles without the guide, open Workflow or Blackboard in the top navigation first. An Agent Task waits until you connect an executor as described below.

From the extracted directory, verify example setup without calling a model:

```sh
KAIROS_DAEMON_EXAMPLE_SMOKE=1 sh examples/daemon/run.sh
KAIROS_REVIEW_LOOP_SMOKE=1 sh examples/review-loop/run.sh
```

Examples need `curl`, `jq`, `openssl` and Perl/POSIX. Setup does not execute Tasks; model execution additionally needs an explicitly configured CLI/model and login. See the packaged [Daemon](../examples/daemon/README.md) and [Writer/Checker](../examples/review-loop/README.md) examples.

Compatibility and validation limits, including unsigned/unnotarized macOS packages, are documented in the [release notes](release-notes.md).

## Connect your agent

Install the Kairos Agent Skill from the project where your agent will work. The installer requires Node.js and npm:

```sh
npx skills add ScienJus/kairos --skill kairos-agent
```

Choose your target agents and project installation scope when prompted. A source checkout or a new package with `.agents/skills/kairos-agent` already includes the Skill for sessions opened in that directory.

The Skill supplies execution instructions. Start Core and configure MCP separately. A repository checkout is not required for an Agent working in another project.

### Codex: local Trusted Mode

In the project where the Agent will work, create `.codex/config.toml` (create `.codex/` if needed) and add this entry. If the file already exists, edit it without replacing unrelated settings or duplicating an existing `kairos` entry:

```toml
[mcp_servers.kairos]
url = "http://127.0.0.1:8080/mcp"
env_http_headers = { "X-Kairos-Actor-Id" = "KAIROS_ACTOR_ID", "X-Kairos-Actor-Kind" = "KAIROS_ACTOR_KIND", "X-Kairos-Actor-Role" = "KAIROS_ACTOR_ROLE" }
enabled = true
```

Replace the URL when using a different port or host. Start the Codex CLI from that project:

```sh
KAIROS_ACTOR_ID=my-project-agent-1 \
KAIROS_ACTOR_KIND=agent \
KAIROS_ACTOR_ROLE=contributor \
codex
```

Trust the project when prompted so Codex can load its project configuration. Use a different Actor ID for each concurrent executor; the Role must match the Task's allowed roles (`contributor` in the quickstart). These Trusted Mode IDs do not need token provisioning. The environment variables must reach the process hosting the MCP connection; setting them in a terminal does not update an already running desktop app. See [Codex MCP configuration](https://developers.openai.com/codex/mcp) for client configuration scope.

### Authenticated Mode and other clients

For an Authenticated server, have an administrator issue an **Agent** Identity Token with the required Role using the console's Token management page. Replace the Trusted Mode entry with:

```toml
[mcp_servers.kairos]
url = "https://YOUR_KAIROS_HOST/mcp"
bearer_token_env_var = "KAIROS_IDENTITY_TOKEN"
enabled = true
```

Set `KAIROS_IDENTITY_TOKEN` in the Codex process environment using your issued Token, then launch Codex. Keep Tokens out of committed configuration; do not send Trusted identity headers alongside Bearer authentication. Identity lifecycle belongs to the [API Reference](api-reference.md#identity-and-authentication).

For another client, configure **Streamable HTTP**, the same `/mcp` URL, and transport identity: local Trusted Mode sends `X-Kairos-Actor-Id`, `X-Kairos-Actor-Kind: agent`, and `X-Kairos-Actor-Role`; Authenticated Mode sends `Authorization: Bearer <Agent Identity Token>`. Follow that client's instructions for environment expansion and reloading MCP configuration. Installing the Skill alone does not install these settings.

### Verify the connection

In Codex, use `/mcp` to confirm `kairos` is connected. Then ask:

```text
Use $kairos-agent to find and complete one available Task.
```

The Agent should call `find_work`. An empty candidate list means the connection worked but there is no eligible work for that identity; check the WorkItem and Task roles in the console. Missing tools indicate a client configuration problem; `401` indicates missing/invalid authentication. Check the endpoint, identity, project trust and client reload before retrying. For the complete two-Agent example, follow the [quickstart](../examples/quickstart/README.md).
