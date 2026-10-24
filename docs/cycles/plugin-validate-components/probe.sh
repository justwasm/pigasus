#!/usr/bin/env bash
# probe.sh — regenerate the plugin-validate fixtures and diff the clone
# against the live reference binary.
#
# Usage:  bash docs/cycles/plugin-validate-components/probe.sh [path-to-wagyu]
#
# Creates a throw-away tree under ${TMPDIR:-/tmp}/pvfix and reports any
# stdout/stderr/exit-code mismatch. Requires `agy` on PATH (or AGY=...).
set -u

AGY=${AGY:-$HOME/.local/bin/agy}
WAGYU=${1:-/tmp/wagyu}
B=${TMPDIR:-/tmp}/pvfix
rm -rf "$B"; mkdir -p "$B"

manifest() { # dir name
  mkdir -p "$B/$1"
  printf '{"name":"%s","description":"d","author":"a"}\n' "$2" > "$B/$1/plugin.json"
}
skill() { # dir rel body
  mkdir -p "$B/$1/$(dirname "$2")"
  printf '%s' "$3" > "$B/$1/$2"
}

# --- skills ---------------------------------------------------------------
manifest s_good s_good
skill s_good skills/ok/SKILL.md $'---\nname: ok\ndescription: fine\n---\nbody\n'
manifest s_nofm s_nofm
skill s_nofm skills/greet/SKILL.md $'# Greet\n\nSay hi.\n'
manifest s_badyaml s_badyaml
skill s_badyaml skills/x/SKILL.md $'---\nname: [unclosed\n---\nbody\n'
manifest s_noname s_noname
skill s_noname skills/y/SKILL.md $'---\ndescription: hi\n---\nbody\n'
manifest s_dup s_dup
skill s_dup skills/one/SKILL.md $'---\nname: same\ndescription: d\n---\nB\n'
skill s_dup skills/two/SKILL.md $'---\nname: same\ndescription: d\n---\nB\n'
manifest s_big s_big
mkdir -p "$B/s_big/skills/big"
{ printf -- '---\nname: big\ndescription: hi\n---\n'; head -c 24001 /dev/zero | tr '\0' a; } \
  > "$B/s_big/skills/big/SKILL.md"

# --- agents ---------------------------------------------------------------
manifest a_noname a_noname; mkdir -p "$B/a_noname/agents"
printf -- '---\ndescription: d\n---\nB\n' > "$B/a_noname/agents/x.md"
manifest a_reserved a_reserved; mkdir -p "$B/a_reserved/agents"
printf -- '---\nname: review-agent\ndescription: d\n---\nB\n' > "$B/a_reserved/agents/x.md"
manifest a_json a_json; mkdir -p "$B/a_json/agents/h"
printf '{"name":"h","description":"d"}\n' > "$B/a_json/agents/h/agent.json"
manifest a_badjson a_badjson; mkdir -p "$B/a_badjson/agents/h"
printf '{bad json\n' > "$B/a_badjson/agents/h/agent.json"
manifest a_nofm a_nofm; mkdir -p "$B/a_nofm/agents"
printf 'body\n' > "$B/a_nofm/agents/x.md"

# --- rules ----------------------------------------------------------------
manifest r_noglobs r_noglobs; mkdir -p "$B/r_noglobs/rules"
printf -- '---\ntrigger: glob\n---\nR\n' > "$B/r_noglobs/rules/r.md"
manifest r_md r_md; mkdir -p "$B/r_md/rules"
printf -- '---\ntrigger: model_decision\n---\nR\n' > "$B/r_md/rules/r.md"
manifest r_notrig r_notrig; mkdir -p "$B/r_notrig/rules"
printf -- '---\ndescription: d\n---\nR\n' > "$B/r_notrig/rules/r.md"
manifest r_readme r_readme; mkdir -p "$B/r_readme/rules"
printf 'readme\n' > "$B/r_readme/rules/README.md"
manifest r_root r_root
printf 'x\n' > "$B/r_root/AGENTS.md"

# --- manifest / schema / hooks / mcp -------------------------------------
manifest f_unknown f_unknown
printf '{"name":"f_unknown","bogus":1,"zzz":2}\n' > "$B/f_unknown/plugin.json"
manifest f_noname f_noname
printf '{"description":"d"}\n' > "$B/f_noname/plugin.json"
manifest f_badname f_badname
printf '{"name":"Bad Name"}\n' > "$B/f_badname/plugin.json"
manifest f_spec f_spec
printf '{"name":"spec_true","$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"}\n' > "$B/f_spec/plugin.json"
manifest f_schema2 f_schema2
printf '{"name":"schema2","$schema":"https://agent-plugins.org/schemas/9.9.9/plugin.schema.json"}\n' > "$B/f_schema2/plugin.json"
manifest h_legacy h_legacy
printf '{"PreToolUse":{"command":"true"},"Stop":{"command":"true"}}\n' > "$B/h_legacy/hooks.json"
manifest h_modern h_modern
printf '{"hooks":{"PreToolUse":[{"matcher":"*","command":"true"}]}}\n' > "$B/h_modern/hooks.json"
manifest m_count m_count
printf '{"mcpServers":{"a":{"command":"echo"},"b":{"command":"echo"}}}\n' > "$B/m_count/mcp_config.json"
mkdir -p "$B/caps"; printf '{"name":"caps"}\n' > "$B/caps/Plugin.json"

# --- suggestions / BOM / exclusions / hidden dir -------------------------
manifest f_dym f_dym
printf '{"name":"f_dym","descripton":"d","auther":"a","nam":"x","unknown_thing":1}\n' > "$B/f_dym/plugin.json"
manifest bom_manifest bom_manifest
printf '\xef\xbb\xbf{"name":"bom_manifest"}\n' > "$B/bom_manifest/plugin.json"
manifest bom_hooks bom_hooks
printf '\xef\xbb\xbf{"PreToolUse":{"command":"true"}}\n' > "$B/bom_hooks/hooks.json"
manifest bom_skill bom_skill
mkdir -p "$B/bom_skill/skills/s"
printf '\xef\xbb\xbf---\nname: s\ndescription: d\n---\nB\n' > "$B/bom_skill/skills/s/SKILL.md"
manifest ex_skills ex_skills
mkdir -p "$B/ex_skills/skills/a" "$B/ex_skills/skills/b"
printf 'no fm\n' > "$B/ex_skills/skills/a/SKILL.md"
printf -- '---\nname: b\ndescription: d\n---\nB\n' > "$B/ex_skills/skills/b/SKILL.md"
printf '{"exclude":["a"]}\n' > "$B/ex_skills/skills.json"
manifest ex_rules ex_rules
mkdir -p "$B/ex_rules/rules"
printf -- '---\ndescription: d\n---\nR\n' > "$B/ex_rules/rules/a.md"
printf -- '---\ntrigger: always_on\n---\nR\n' > "$B/ex_rules/rules/b.md"
printf '{"exclude":["a"]}\n' > "$B/ex_rules/rules.json"
manifest ex_agents ex_agents
mkdir -p "$B/ex_agents/agents"
printf -- '---\nname: a\n---\nB\n' > "$B/ex_agents/agents/a.md"
printf -- '---\nname: b\ndescription: d\n---\nB\n' > "$B/ex_agents/agents/b.md"
printf '{"exclude":["a"]}\n' > "$B/ex_agents/agents.json"
mkdir -p "$B/hid/.antigravity-plugin"
printf '{"name":"hid"}\n' > "$B/hid/.antigravity-plugin/plugin.json"
mkdir -p "$B/hid/.antigravity-plugin/skills/s"
printf -- '---\nname: s\ndescription: d\n---\nB\n' > "$B/hid/.antigravity-plugin/skills/s/SKILL.md"
printf '{"PreToolUse":{"command":"true"}}\n' > "$B/hid/.antigravity-plugin/hooks.json"
printf '{"mcpServers":{"a":{"command":"echo"}}}\n' > "$B/hid/.antigravity-plugin/mcp_config.json"
manifest bad_trunc bad_trunc
printf '{"agents": [{"name": "x"' > "$B/bad_trunc/agents.json"

fail=0
for d in "$B"/*/; do
  d=${d%/}; name=$(basename "$d")
  for mode in "" "--json" "--strict"; do
    ( cd /tmp && "$AGY" plugin validate "$d" $mode >"$B/ref.out" 2>"$B/ref.err" ); r1=$?
    ( cd /tmp && "$WAGYU" plugin validate "$d" $mode >"$B/clo.out" 2>"$B/clo.err" ); r2=$?
    if ! diff -q "$B/ref.out" "$B/clo.out" >/dev/null \
       || ! diff -q "$B/ref.err" "$B/clo.err" >/dev/null \
       || [ "$r1" != "$r2" ]; then
      echo "MISMATCH $name ${mode:-<human>} (ref=$r1 clone=$r2)"
      diff "$B/ref.out" "$B/clo.out" | head -10
      diff "$B/ref.err" "$B/clo.err" | head -5
      fail=1
    fi
  done
done

[ "$fail" = 0 ] && echo "all fixtures match ($B)"
exit "$fail"
