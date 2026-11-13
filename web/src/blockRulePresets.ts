import type { BlockRule } from './types';

// Conditions are ORed, so reordering them does not customize protection.
export function hasPresetConditions(rule: BlockRule, preset: BlockRule): boolean {
  return rule.conditions.length === preset.conditions.length && preset.conditions.every((condition) =>
    rule.conditions.some((current) => current.operator === condition.operator && current.value === condition.value));
}

// Restoring conditions must never reset an operator's scope, ban, name or
// disabled choice. Missing presets retain the server's default enabled state.
export function withPresetConditions(rules: readonly BlockRule[], preset: BlockRule): BlockRule[] {
  const existing = rules.some((rule) => rule.id === preset.id);
  const conditions = preset.conditions.map((condition) => ({ ...condition }));
  return existing
    ? rules.map((rule) => rule.id === preset.id ? { ...rule, conditions } : rule)
    : [...rules, { ...preset, hosts: [...preset.hosts], conditions }];
}
