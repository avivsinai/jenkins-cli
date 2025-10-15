# Agent Cookbook: Efficient Jenkins CLI Patterns

This guide captures common automation recipes for agents that integrate with the `jk` CLI. Each pattern includes the CLI call and a lightweight code example (Python unless noted) to embed in bots or orchestrators.

## 1. Latest deployment for a specific chart

**Problem:** Fetch the most recent run that deployed a given Helm chart.

**CLI:**
```bash
jk run search \
  --folder releases \
  --job-glob "*/deploy-*" \
  --filter param.CHART_NAME=nova-video-prod \
  --limit 1 \
  --select parameters --json
```

**Python:**
```python
import json
import subprocess

def latest_chart_deploy(chart_name: str):
    result = subprocess.run([
        "jk", "run", "search",
        "--folder", "releases",
        "--job-glob", "*/deploy-*",
        "--filter", f"param.CHART_NAME={chart_name}",
        "--limit", "1",
        "--select", "parameters",
        "--json",
    ], check=True, capture_output=True, text=True)
    data = json.loads(result.stdout)
    return data["items"][0] if data["items"] else None
```

## 2. Enumerate high-risk failures in the last 24 hours

**Problem:** Surface failing runs across a folder for incident response.

**CLI:**
```bash
jk run search \
  --folder platform \
  --filter result=FAILURE \
  --since 24h \
  --select branch,commit \
  --json
```

**Python:**
```python
import json
import subprocess

def recent_failures():
    result = subprocess.run([
        "jk", "run", "search",
        "--folder", "platform",
        "--filter", "result=FAILURE",
        "--since", "24h",
        "--select", "branch,commit",
        "--json",
    ], check=True, capture_output=True, text=True)
    return json.loads(result.stdout)["items"]
```

## 3. Snapshot job parameters before triggering a run

**Problem:** Populate UI defaults when exposing a manual trigger.

**CLI:**
```bash
jk run params Helm.Chart.Deploy --source auto --json
```

**TypeScript (Deno):**
```ts
const proc = new Deno.Command("jk", {
  args: ["run", "params", "Helm.Chart.Deploy", "--json"],
}).outputSync();
const payload = JSON.parse(new TextDecoder().decode(proc.stdout));
const required = payload.parameters.filter((p: any) => p.frequency >= 0.99);
```

## 4. Build a run summary dashboard

**Problem:** Display success/failed counts for the last week across critical jobs.

**CLI:**
```bash
jk run ls team/api/deploy --since 7d --group-by result --agg count --json
```

**Python:**
```python
import json
import subprocess

summary = json.loads(subprocess.run([
    "jk", "run", "ls", "team/api/deploy",
    "--since", "7d",
    "--group-by", "result",
    "--agg", "count",
    "--json",
], check=True, capture_output=True, text=True).stdout)
counts = {g["value"]: g["count"] for g in summary.get("groups", [])}
```

## 5. Alert on long-running pipelines

**Problem:** Identify runs that exceed a duration threshold.

**CLI:**
```bash
jk run ls team/etl/pipeline --filter duration>=7200000 --since 48h --json
```

**Python:**
```python
import json
import subprocess

slow = json.loads(subprocess.run([
    "jk", "run", "ls", "team/etl/pipeline",
    "--filter", "duration>=7200000",
    "--since", "48h",
    "--json",
], check=True, capture_output=True, text=True).stdout)["items"]
```

## 6. Retrieve metadata hints for adaptive prompts

**Problem:** Give an agent the list of available filters and parameters for a job.

**CLI:**
```bash
jk run ls team/api/deploy --with-meta --json --limit 0
```

**Python:**
```python
import json
import subprocess

meta = json.loads(subprocess.run([
    "jk", "run", "ls", "team/api/deploy",
    "--with-meta", "--json", "--limit", "0"
], check=True, capture_output=True, text=True).stdout)["metadata"]
```

## 7. Detect queued runs stuck longer than 15 minutes

**Problem:** Alert SREs when runs linger in the queue.

**CLI:**
```bash
jk run search --folder platform --filter queue.id>0 --select queueId --json
```

**Python:**
```python
import json
import subprocess

runs = json.loads(subprocess.run([
    "jk", "run", "search",
    "--folder", "platform",
    "--filter", "queue.id>0",
    "--select", "queueId",
    "--json",
], check=True, capture_output=True, text=True).stdout)["items"]
```

## 8. Export change authors for auditing

**Problem:** List recent runs and commits for a compliance report.

**CLI:**
```bash
jk run ls compliance/pipeline --select branch,commit --limit 20 --json
```

**Python:**
```python
import json
import subprocess

runs = json.loads(subprocess.run([
    "jk", "run", "ls", "compliance/pipeline",
    "--select", "branch,commit",
    "--limit", "20",
    "--json",
], check=True, capture_output=True, text=True).stdout)["items"]
```

## 9. Monitor multibranch deployments

**Problem:** Track the latest deployment per branch in a multibranch pipeline.

**CLI:**
```bash
jk run ls team/website/deploy --group-by branch --agg last --json
```

**Python:**
```python
import json
import subprocess

branches = json.loads(subprocess.run([
    "jk", "run", "ls", "team/website/deploy",
    "--group-by", "branch",
    "--agg", "last",
    "--json",
], check=True, capture_output=True, text=True).stdout)["groups"]
```

## 10. Feed command catalog into an LLM agent

**Problem:** Provide a model with structured command references.

**CLI:**
```bash
jk help --json > /tmp/jk-help.json
```

**Python:**
```python
with open("/tmp/jk-help.json", "r", encoding="utf-8") as handle:
    command_tree = json.load(handle)
```
