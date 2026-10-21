# TODO

## 1. Automatisch controleren of ssh nog werkt

Wens van de gebruiker (2026-10-21): "automatic check if the ssh still works". Bouwt voort op
idee 2 in `docs/PLAN.md` §9, maar met een echte login in plaats van alleen een TCP-dial.

- [x] Besloten (2026-10-21): een check is een echte login, `ssh -o BatchMode=yes -o ConnectTimeout=5 <host> true`, met de opties uit `sshx.Options` (poort, key, jump host, extra args). Zo vallen ingetrokken keys en verwijderde users ook op.
- [x] Besloten (2026-10-21) tijdens het bouwen: `p` controleert de geselecteerde host opnieuw, `P` alle hosts. `haven check <query>` controleert alle hosts die matchen (geen picker). `haven edit <q> --no-check` zonder andere flags opent nog steeds het formulier; `--no-check` slaat alleen de check over.
- [x] Besloten (2026-10-21): hosts worden tegelijk gecontroleerd, maximaal 8 tegelijk. Een host wordt automatisch hooguit eens per 5 minuten opnieuw gecontroleerd.
- [x] Nieuwe migratie `003_…sql`: `host_state` krijgt `last_check_at`, `last_check_ok` en `last_check_error`. In `internal/store/state.go` komen functies om het resultaat op te slaan en te lezen.
- [x] Een package (bijvoorbeeld `internal/check`) dat één host of een lijst hosts controleert en per host OK, fout en foutmelding teruggeeft. De ssh-aanroep moet in tests te vervangen zijn.
- [x] `haven check [query] [--tag]` controleert de gekozen hosts (zonder argument: alle hosts), print een tabel `NAME  TARGET  STATUS  ERROR` en eindigt met exit code 1 als er één faalt, zodat het in cron of een script werkt. `--json` voor scripts. Voeg `check` toe aan `reserved()`.
- [x] Na `haven add` en `haven edit` (ook via het formulier): de nieuwe of gewijzigde verbinding wordt geprobeerd en bij een fout komt er een waarschuwing op stderr. De host wordt hoe dan ook opgeslagen. Met `--no-check` sla je de check over.
- [x] Host manager (`internal/tui/hosts`): bij openen worden alle hosts op de achtergrond gecontroleerd, behalve de hosts die net nog gecontroleerd zijn. Per host komt een stip (groen OK, rood fout, grijs onbekend of bezig). Het detailpaneel toont de laatste check en de foutmelding. De opgeslagen status is direct zichtbaar bij het opstarten. Een toets (bijvoorbeeld `p`) controleert opnieuw, en die staat ook in de help.
- [x] `haven ls` krijgt een kolom met de laatste status, en `--json` bevat de check-velden.
- [x] README: `haven check`, de stip in de host manager en `--no-check` beschrijven.
- [x] Tests: store-migratie en opslag, het check-package met een nep-ssh, `haven check` (tabel, exit code, `--json`), de waarschuwing na add, en teatest-snapshots van de hosts-lijst met stippen. Controleer daarna in een echte terminal (desktop-breedte 120 kolommen en smal 60 kolommen, als vervanging voor de telefoonbreedte) dat de stippen en het detailpaneel goed staan.
