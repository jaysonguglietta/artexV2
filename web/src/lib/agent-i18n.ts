import { useCallback, useEffect, useMemo, useState } from "react";

import { useTranslations } from "use-intl";

import { api } from "@/lib/api";
import type { Agent } from "@/lib/types";

// Built-in agent names, descriptions and variable labels may come from legacy database seed text.
// Older seeds include names in their original language, including the retester.
// Seeds use ON CONFLICT DO NOTHING and are not rewritten in existing installations.
// Look up presentation labels by stable agent key
// in the English catalog's builtinAgents namespace,
// falling back to the database value. User-created agents retain user-authored text.

// The retester is seeded with builtin=false but has a fixed system-owned key,
// so it is still eligible for translated presentation labels. Other built-ins
// are identified by agent.builtin.
const SYSTEM_SEEDED_AGENT_KEYS = new Set(["retester"]);

function isSystemSeeded(agent: { key: string; builtin?: boolean }): boolean {
  return !!agent.builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agent.key);
}

// The system-seeded reporter remains editable in the UI.
// Translate it only while its value exactly matches the historical seed; preserve user edits.
const EDITABLE_SEEDS: Record<string, { name: string; description: string }> = {
  reporter: {
    name: "보고서 작성",
    description:
      "취약점 상세 보고서 작성: 취약점을 발견하면 자동으로 트리거되어, 증거와 실행 과정을 조회한 뒤 Markdown 보고서를 작성해 해당 취약점에 기록합니다.",
  },
};

// useAgentLabels supplies English built-in names, descriptions and variable labels.
// All presentation sites use these helpers.
export function useAgentLabels() {
  const t = useTranslations("builtinAgents");

  // Consumers use these callbacks as memoization dependencies for groups and filters,
  // so create a stable bundle unless the translation function changes.
  return useMemo(() => {
    // Return the English built-in name or the user-authored database name.
    const name = (agent: { key: string; name: string; builtin?: boolean }): string => {
      const translatable = isSystemSeeded(agent) || EDITABLE_SEEDS[agent.key]?.name === agent.name;
      if (translatable && t.has(`${agent.key}.name`)) return t(`${agent.key}.name`);
      return agent.name;
    };

    // Return the English built-in description, otherwise the database description or empty text.
    // The caller retains responsibility for an empty-description placeholder.
    const description = (agent: { key: string; description?: string; builtin?: boolean }): string => {
      const translatable = isSystemSeeded(agent) || EDITABLE_SEEDS[agent.key]?.description === agent.description;
      if (translatable && t.has(`${agent.key}.description`)) return t(`${agent.key}.description`);
      return agent.description ?? "";
    };

    // nameByKey handles sites with a key but no Agent object, such as conversation grouping.
    // Use the supplied lookup; missing agents fall back to the stable key.
    const nameByKey = (
      key: string,
      lookup: (key: string) => { key: string; name: string; builtin?: boolean } | undefined,
    ): string => {
      const agent = lookup(key);
      // biome-ignore lint/nursery/useNullishCoalescing: Empty names intentionally fall back to the key, so logical OR is required.
      return (agent && name(agent)) || key;
    };

    // Translate a built-in prompt-variable label when known,
    // otherwise keep its stored fallback unchanged.
    const varLabel = (agentKey: string, builtin: boolean, varName: string, fallback: string): string => {
      if ((builtin || SYSTEM_SEEDED_AGENT_KEYS.has(agentKey)) && t.has(`${agentKey}.vars.${varName}`)) {
        return t(`${agentKey}.vars.${varName}`);
      }
      return fallback;
    };

    return { name, description, nameByKey, varLabel };
  }, [t]);
}

// Several screens need only key-to-display-name conversion,
// so share one module-level agent-list request and retry after failure.
let agentListPromise: Promise<Agent[]> | null = null;
function loadAgentList(): Promise<Agent[]> {
  if (!agentListPromise) {
    agentListPromise = api.agents().catch((err) => {
      agentListPromise = null;
      throw err;
    });
  }
  return agentListPromise;
}

// useAgentNameByKey supports approval/intercept records with only conv_agent_key.
// Built-in agents use English names and custom agents retain stored names;
// unknown or not-yet-loaded agents show their keys.
export function useAgentNameByKey(): (key: string) => string {
  const labels = useAgentLabels();
  const [agents, setAgents] = useState<Map<string, Agent>>(() => new Map());
  useEffect(() => {
    let alive = true;
    loadAgentList()
      .then((list) => {
        if (alive) setAgents(new Map(list.map((a) => [a.key, a])));
      })
      .catch(() => {
        // Keep keys visible when loading fails; retry on the next mount.
      });
    return () => {
      alive = false;
    };
  }, []);
  return useCallback((key: string) => labels.nameByKey(key, (k) => agents.get(k)), [labels, agents]);
}
