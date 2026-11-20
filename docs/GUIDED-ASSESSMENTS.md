# Guided website assessments

Choose **Tasks → New assessment → Website check**. Enter the website address,
choose an AI provider, confirm that you own the target or have permission, and
choose **Save and review setup**. The assessment is saved **paused** with a
15-minute budget. Saving does not call a model or run assessment tools.

The starter checks HTTPS, response headers, cookie attributes, and at most five
ordinary public links on the same host. It excludes exploitation, credential
attacks, forms, uploads, broad scanning and third-party hosts. These instructions
are model guidance, not a technical rate limiter or hostname firewall. The
isolated worker's operator-controlled IP policy remains the network boundary.

## Setup and start

The Overview shows whether the scanning service can be reached and whether it
has a target policy for this assessment. **Check setup** refreshes both checks.
It does not contact the target, launch a container, validate model billing, test
DNS/TLS, or prove a scan worked. The API and broker must both be updated for this
feature; an older broker appears unavailable until rebuilt.

If target permission is missing, expand **How to finish setup** and use **Copy
target approval command**. Run the copied command from your ARTEX source folder
in Terminal, with the Docker context used for your installation. For the assisted
Mac installation, set `export DOCKER_CONTEXT=colima-artex-test` first.

The `scripts/approve-website.py` helper locates the running broker bound to that
checkout's exact policy path, resolves the website's public IPv4 addresses,
shows them for review, and requires typing `APPROVE`. It preserves management
deny rules and other tasks, keeps a backup of the previous policy, and restarts
only that broker. It never starts an assessment. It refuses private or denied
addresses. IPv6-only and private targets require separate operator review.

Approval is for the resolved IPs, not only the hostname. Review shared-hosting or
CDN addresses separately. DNS changes require another reviewed approval. Never
approve addresses solely because an agent suggests them. Broker firewall deny
rules still take precedence; a configured policy does not guarantee reachability.

Return to Overview, choose **Check setup**, then **Start assessment**. Provider
usage can incur charges. Existing advanced creation and lifecycle controls remain
available; they are not replaced by the guided checklist.

## Follow progress and recover

**View execution details** opens the task sessions, where you can review actual
tool calls, responses and failures. The Overview highlights a recent failed
operation when present in the loaded activity window; this is not a complete
execution audit and may refer to an older attempt. Use execution details for the
full history. Use **Check setup** to refresh the summary after a run ends.

A completed task with no findings is not proof that the website was tested. The
report now includes up to 50 agent-recorded observation summaries and an explicit
coverage limitation. Those summaries can describe blocked checks and are not
independent verification. Review underlying responses before relying on results.

For an ended website task, after setup checks pass, **Prepare a homepage recheck**
opens an editable follow-up. Nothing starts until **Add follow-up and restart** is
chosen. That action resumes planning and may incur provider charges. Existing
scope restrictions must be preserved. Other task configuration is under
**Advanced task controls and diagnostics** on the Overview.

For manual installation, worker deployment and recovery, see
[Installation](INSTALLATION.md) and [Worker isolation](WORKER-ISOLATION.md).
