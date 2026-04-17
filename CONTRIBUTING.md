# Contribuindo

Obrigado pelo interesse em contribuir. Este documento resume o fluxo de trabalho para propor mudanças neste repositório.

## Antes de Começar

- Busque na lista de issues se o problema já está mapeado.
- Para mudanças significativas (nova API pública, quebra de compatibilidade, refatoração arquitetural), abra uma issue de discussão antes de codar.
- Consulte o [GAPS_ROADMAP.md](../GAPS_ROADMAP.md) da organização para entender o contexto do ecossistema.

## Fluxo de Trabalho

1. Fork + branch a partir de `main`.
2. Commit em mensagens claras no padrão [Conventional Commits](https://www.conventionalcommits.org/). Exemplos:
   - `feat(router): add rate-limit middleware`
   - `fix(mediator): handle cancellation in streaming`
   - `chore(governance): add dependabot config`
3. Teste localmente — todo PR precisa passar o pipeline de CI.
4. Abra PR com descrição contendo: motivação, resumo técnico, checklist aplicável, screenshots/exemplos quando relevante.
5. Revisão mínima de 1 mantenedor antes de merge.

## Padrões Técnicos

- **.NET:** código compila com `TreatWarningsAsErrors=true` e `Nullable=enable`. AOT-compatibility obrigatória para libs publicadas (`IsAotCompatible=true`).
- **TypeScript:** `strict` no tsconfig, sem `any` implícito, exports tipados em `package.json`.
- **Go:** `go vet`, `golangci-lint`, `gofmt` limpos.
- **Cobertura mínima:** 85% (line + branch) onde há `coverlet.runsettings` ou `vitest --coverage`.

## Quebra de Compatibilidade

Mudanças de API pública exigem:

- Descrição explícita no corpo do PR.
- Atualização de `CHANGELOG.md` sob seção "Breaking Changes".
- Onde existir `PublicAPI.Shipped.txt`, atualizar e justificar.

## Licença

Ao contribuir, você concorda que sua contribuição será licenciada sob os mesmos termos do projeto (ver `LICENSE`).

## Contato

Dúvidas não-técnicas ou proposta de parceria: ops@swepay.com.br.
Vulnerabilidades: security@swepay.com.br (ver `SECURITY.md`).
