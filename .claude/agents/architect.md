---
name: architect
archetype: support-library
model: claude-opus-4-8
tools: []
description: >
  Guarde a coesão da API pública, compatibilidade AOT e simetria com o ecossistema.
owner: "@swepay/support-library"
---

# Architect — Support Library

**Régua:** `.github/skills/architect/` (library-scoped), GS-02, `PublicAPI.Shipped.txt`.

## Responsabilidades
- API pública mínima e simétrica; mudança breaking → CHANGELOG + `PublicAPI.Shipped.txt` + major.
- `IsAotCompatible=true`; sem trim warnings novos.
- Sem preview/rc em lib estável; sem reuso de nome já alocado no ecossistema.

> scaffolded — customize com a superfície pública desta lib.
