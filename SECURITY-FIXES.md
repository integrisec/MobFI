# MobFI Security-Fix Changelog

Fix-tracking log for the findings enumerated in
`SECURITY-AUDIT.md` and `LICENSE-AUDIT.md`.

Each row corresponds to one finding. Statuses:

- `Open` -- not yet touched.
- `In progress` -- work started, not merged.
- `Fixed` -- remediated in this branch; commit hash in
  the Fix column.
- `Deferred` -- acknowledged, deliberately postponed;
  reason in Notes.
- `Wont fix` -- rejected as intended behaviour or the
  wrong tradeoff; reason in Notes.
- `N/A` -- superseded by another fix; Notes says which.

Branch: `security-audit-2026-07-31`.
Baseline commit (main HEAD at audit time): `b9c42e3`.

## Security findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| MFI-UPD-01 | Critical | Open | | Requires operator to set up signing infrastructure; code enforces verification. |
| MFI-PATH-01 | Critical | Open | | |
| MFI-CMD-01 | Critical | Open | | |
| MFI-CMD-02 | High | Open | | |
| MFI-CMD-03 | High | Open | | |
| MFI-CMD-04 | High | Open | | |
| MFI-CMD-05 | High | Open | | |
| MFI-PATH-02 | High | Open | | |
| MFI-UPD-02 | High | Open | | |
| MFI-UPD-03 | High | Open | | |
| MFI-GUI-01 | High | Open | | |
| MFI-GUI-02 | High | Open | | |
| MFI-GUI-03 | High | Open | | |
| MFI-PAR-01 | High | Open | | |
| MFI-PAR-02 | High | Open | | |
| MFI-PAR-03 | High | Open | | |
| MFI-PAR-04 | High | Open | | |
| MFI-SEC-01 | High | Open | | |
| MFI-XC-01 | High | Open | | |
| MFI-XC-02 | High | Open | | |
| MFI-DEP-01 | High | Open | | |
| MFI-DEP-02 | High | Open | | |
| MFI-UPD-04 | Medium | Open | | |
| MFI-UPD-05 | Medium | Open | | |
| MFI-UPD-06 | Medium | Open | | |
| MFI-GUI-04 | Medium | Open | | |
| MFI-GUI-05 | Medium | Open | | |
| MFI-PAR-05 | Medium | Open | | |
| MFI-PAR-06 | Medium | Open | | |
| MFI-PAR-07 | Medium | Open | | |
| MFI-SEC-02 | Medium | Open | | |
| MFI-SEC-03 | Medium | Open | | |
| MFI-SEC-04 | Medium | Open | | |
| MFI-XC-03 | Medium | Open | | |
| MFI-XC-04 | Medium | Open | | |
| MFI-XC-05 | Medium | Open | | |
| MFI-XC-06 | Medium | Open | | |
| MFI-CMD-06 | Medium | Open | | |
| MFI-DEP-03 | Medium | Open | | |
| MFI-UPD-07 | Low | Open | | |
| MFI-GUI-06 | Low | Open | | |
| MFI-GUI-07 | Low | Open | | |
| MFI-GUI-08 | Low | Open | | |
| MFI-PAR-08 | Low | Open | | |
| MFI-PAR-09 | Low | Open | | |
| MFI-SEC-05 | Low | Open | | |
| MFI-XC-07 | Low | Open | | |
| MFI-XC-08 | Low | Open | | |
| MFI-PATH-03 | Low | Open | | |
| MFI-PATH-04 | Low | Open | | |
| MFI-CMD-07 | Low | Open | | |
| MFI-UPD-08 | Info | Open | | |
| MFI-UPD-09 | Info | Open | | |
| MFI-CMD-08 | Info | Open | | |
| MFI-CMD-09 | Info | Open | | |
| MFI-CMD-10 | Info | Open | | |

## License findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| LIC-01 | Medium | Open | | |
| LIC-02 | Medium | Open | | |
| LIC-03 | Medium | Open | | |
| LIC-04 | Low | Open | | |
| LIC-05 | Low | Open | | |

## Notes

Rows update in the same commit that lands the fix, so the
tracker and the code stay in sync. `Fix` column holds the
short SHA of the commit that closes the finding.
