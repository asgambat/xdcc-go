# Piano di Implementazione — Modalità Client-Server XDCC-Go

## Panoramica

Aggiunta di una modalità client-server all'attuale tool CLI. Il server gestisce connessioni IRC persistenti, code di download e logica di retry. Il client è una web app responsive (PWA) che comunica col server via REST API.

---

## Architettura Generale

```
┌──────────────────────────────┐       ┌──────────────────────────────────┐
│         WEB CLIENT           │       │            SERVER                 │
│  (SPA PWA - responsive)      │◄─────►│  REST API + SSE stream           │
│  HTML/CSS/JS (embedded)      │       │                                  │
└──────────────────────────────┘       │  ┌────────────────────────────┐  │
                                       │  │   IRC Connection Manager   │  │
                                       │  │  - Persistent connections  │  │
                                       │  │  - Auto-reconnect          │  │
                                       │  │  - Channel management      │  │
                                       │  └────────────────────────────┘  │
                                       │  ┌────────────────────────────┐  │
                                       │  │   Download Queue Manager   │  │
                                       │  │  - 1 download/channel      │  │
                                       │  │  - Parallel tra canali     │  │
                                       │  │  - Persistenza stato       │  │
                                       │  └────────────────────────────┘  │
                                       │  ┌────────────────────────────┐  │
                                       │  │   Search Aggregator        │  │
                                       │  │  - Query parallele         │  │
                                       │  │  - Timeout configurabile   │  │
                                       │  └────────────────────────────┘  │
                                       └──────────────────────────────────┘
```

---

## Punti di Implementazione

### Fase 1 — Configurazione e Struttura Progetto

- [ ] **1.1** Creare il file di configurazione `config.yaml` con struttura per: server IRC di default (con canali), directory di download (temp e destinazione), porta HTTP del server, timeout ricerca provider, page size di default, credenziali IRC (nickname base).
- [ ] **1.2** Creare il package `internal/config` che carica e valida la configurazione da file YAML + variabili d'ambiente + flag CLI. le variabili di ambiente vincono su tutto, poi i flag cli ed infine viene il file YAML.
- [ ] **1.3** Creare la struttura del comando `cmd/xdcc-server/main.go` con cobra, che avvia il server HTTP e il gestore connessioni IRC. Deve accettare flag `--config`, `--port`, `--download-dir`, `--temp-dir`.
- [ ] **1.4** Aggiornare `go.mod` con le nuove dipendenze: un router HTTP: `chi` , `go-yaml`, `modernc.org/sqlite` (CGO-free). Nota: SSE non richiede librerie aggiuntive, si implementa con la stdlib.

### Fase 2 — Persistenza (SQLite)

- [ ] **2.1** Creare il package `internal/store` con interfaccia per la persistenza. Usare SQLite (CGO-free con `modernc.org/sqlite`) come backend.
- [ ] **2.2** Definire lo schema del database:
  - Tabella `irc_servers`: id, address, port, auto_connect (bool), status, last_connected_at, retry_count.
  - Tabella `irc_channels`: id, server_id (FK), name, auto_join (bool), topic, joined (bool).
  - Tabella `downloads`: id, pack_message, bot, server_address, channel, filename, filesize, status (queued/downloading/completed/failed/paused), progress_bytes, speed_bps, created_at, started_at, completed_at, error_message, priority/position.
  - Tabella `config_kv`: key, value (per configurazione runtime).
- [ ] **2.3** Implementare le operazioni CRUD per servers, channels e downloads. Metodi: `EnqueueDownload`, `GetQueue(channel)`, `UpdateProgress`, `MarkCompleted`, `MarkFailed`, `GetActiveDownloads`, `GetPendingByChannel`, `RecoverOnStartup`.
- [ ] **2.4** Implementare la logica di recovery: all'avvio del server, i download con status `downloading` vengono rimessi in coda come `queued` per essere ritentati.

### Fase 3 — IRC Connection Manager

- [ ] **3.1** Creare il package `internal/ircmanager` che gestisce connessioni IRC persistenti multiple (una per server). Deve riusare la libreria `girc` già in uso.
- [ ] **3.2** Implementare la connessione automatica ai server di default (da config) all'avvio del server.
- [ ] **3.3** Implementare il join automatico ai canali di default per ciascun server.
- [ ] **3.4** Implementare la logica di reconnect con backoff esponenziale: al fallimento della connessione o disconnessione, ritentare fino a 5 volte con delay esponenziale (es. 5s, 10s, 20s, 40s, 80s). Dopo 5 fallimenti, ritentare ogni ora.
- [ ] **3.5** Esporre metodi pubblici: `ConnectServer(address, port)`, `DisconnectServer(id)`, `JoinChannel(serverId, channel)`, `LeaveChannel(serverId, channel)`, `GetServers()`, `GetChannels(serverId)`, `GetChannelTopic(serverId, channel)`.
- [ ] **3.6** Emettere eventi (via channel Go o callback) per cambiamenti di stato: server connected/disconnected, channel joined/left, topic updated. Questi eventi saranno propagati ai client via SSE.

### Fase 4 — Download Queue Manager

- [ ] **4.1** Creare il package `internal/queue` che gestisce la coda di download. Regola: max 1 download attivo per canale IRC, download paralleli tra canali diversi.
- [ ] **4.2** Implementare `Enqueue(pack)`: aggiunge alla coda. Se nessun download è attivo per quel canale, avvia subito; altrimenti mette in coda.
- [ ] **4.3** Implementare `onDownloadComplete(channel)`: quando un download finisce (successo o fallimento), prende il prossimo dalla coda dello stesso canale e lo avvia.
- [ ] **4.4** Integrare con il client IRC esistente (`internal/irc`) per eseguire il download effettivo. Riusare la logica di `DownloadAll` adattandola per operare su singoli pack con reporting del progresso via callback.
- [ ] **4.5** Implementare il reporting del progresso in tempo reale: bytes scaricati, velocità, ETA per aggiornare il client via SSE.
- [ ] **4.6** Implementare la persistenza della coda: ogni cambio di stato (enqueue, start, progress, complete, fail) viene scritto nel DB SQLite.
- [ ] **4.7** Implementare il recovery all'avvio: leggere dal DB i download incompleti e rimetterli in coda.
- [ ] **4.8** Supportare le directory configurabili: temp dir per file in corso di download, destination dir per file completati. Spostare il file da temp a destination al completamento.

### Fase 5 — Search Aggregator

- [ ] **5.1** Creare il package `internal/searchagg` che esegue ricerche in parallelo su tutti i provider disponibili (nibl, xdcc-eu, subsplease, + eventuali futuri).
- [ ] **5.2** Implementare il timeout configurabile per provider (default 5 secondi). Se un provider non risponde entro il timeout, il suo risultato viene ignorato e si procede con quelli ricevuti.
- [ ] **5.3** Aggregare i risultati: deduplicare (stesso filename + size + bot family), ordinare per rilevanza/dimensione.
- [ ] **5.4** Supportare i filtri già presenti nel CLI:
  - `-p` / `--prefix`: solo risultati il cui filename inizia con il termine di ricerca.
  - `-b` / `--bot`: filtro per nome bot (substring, case-insensitive).
  - `-c` / `--compact`: rimuovi duplicati (stesso filename, size, bot family).
  - `-x` / `--ext`: filtro per estensione file (comma-separated).
- [ ] **5.5** Implementare la paginazione dei risultati: page size configurabile (default 50), restituire page + total count nella risposta API.

### Fase 6 — REST API

- [ ] **6.1** Creare il package `internal/api` con router HTTP. Endpoint:
  - `GET /api/servers` — lista server con stato connessione.
  - `POST /api/servers` — connetti a nuovo server.
  - `DELETE /api/servers/:id` — disconnetti da server.
  - `GET /api/servers/:id/channels` — lista canali per server.
  - `POST /api/servers/:id/channels` — join a canale.
  - `DELETE /api/servers/:id/channels/:name` — leave da canale.
  - `GET /api/servers/:id/channels/:name/topic` — topic del canale.
  - `GET /api/search?q=...&prefix=...&bot=...&ext=...&compact=...&page=...&pageSize=...` — ricerca aggregata.
  - `POST /api/downloads` — avvia/accoda un download (body: pack info).
  - `GET /api/downloads` — lista download attivi + in coda.
  - `DELETE /api/downloads/:id` — cancella/rimuovi download dalla coda.
  - `GET /api/config` — configurazione corrente.
  - `PUT /api/config` — aggiorna configurazione runtime.
- [ ] **6.2** Implementare middleware: CORS, logging, error handling standardizzato (JSON errors).
- [ ] **6.3** Servire i file statici della web app dalla stessa porta HTTP (embedded nel binario con `embed`).

### Fase 7 — SSE (Server-Sent Events) per Aggiornamenti Real-Time

- [ ] **7.1** Implementare endpoint SSE `GET /api/events` che invia eventi in tempo reale ai client connessi. Usa `Content-Type: text/event-stream`. Nessuna configurazione speciale necessaria con reverse proxy.
- [ ] **7.2** Definire i tipi di evento (campo `event:` nel protocollo SSE):
  - `server_status_changed` (connected/disconnected/reconnecting)
  - `channel_joined` / `channel_left` / `channel_topic_updated`
  - `download_started` / `download_progress` / `download_completed` / `download_failed`
  - `download_queued` / `download_removed`
- [ ] **7.3** Implementare hub di broadcast: gestione connessioni SSE multiple (ogni client ha una goroutine dedicata), fan-out degli eventi a tutti i client connessi. Gestione graceful close quando il client si disconnette.
- [ ] **7.4** Per `download_progress`, inviare aggiornamenti a intervalli regolari (es. ogni 500ms) con: bytes scaricati, filesize, velocità, ETA.

### Fase 8 — Web App (Frontend PWA)

- [ ] **8.1** Creare la struttura del frontend in `web/` (HTML, CSS, JS vanilla o framework leggero). Il frontend verrà incorporato nel binario Go con `go:embed`.
- [ ] **8.2** Implementare la pagina principale: lista dei server connessi con indicatore di stato (verde/rosso/giallo per connesso/disconnesso/reconnecting).
- [ ] **8.3** Implementare la vista server: cliccando un server si mostra la lista canali joinati, con possibilità di joinare nuovi canali o lasciare quelli esistenti.
- [ ] **8.4** Implementare la vista canale: cliccando un canale si mostra il topic del canale.
- [ ] **8.5** Implementare la pagina di ricerca: campo di ricerca + filtri (prefix, bot, extension, compact). Risultati paginati (default 50 per pagina). Ogni risultato è cliccabile.
- [ ] **8.6** Implementare l'azione click su risultato di ricerca: cliccando un risultato si avvia/accoda il download del pacchetto. Feedback visivo immediato (toast/snackbar).
- [ ] **8.7** Implementare la pagina downloads: sezione download in corso (con barra di avanzamento, velocità, ETA, filename, dimensione) + sezione download in coda (ordinati per canale e posizione).
- [ ] **8.8** Implementare la connessione SSE (`EventSource`) per ricevere aggiornamenti real-time. Aggiornare la UI senza refresh della pagina. Riconnessione automatica gestita nativamente dal browser.
- [ ] **8.9** Rendere la UI responsive: layout mobile-first con breakpoints per tablet e desktop. Menu hamburger su mobile, sidebar su desktop.
- [ ] **8.10** Implementare il `manifest.json` e service worker per rendere la web app installabile come PWA (Add to Home Screen su Android/iOS).
- [ ] **8.11** Aggiungere la pagina impostazioni: configurazione directory temp/destinazione, timeout ricerca, page size, gestione server di default.

### Fase 9 — Integrazione e Testing

- [ ] **9.1** Integrare tutti i componenti nel `cmd/xdcc-server/main.go`: avvio config → SQLite → IRC manager → download queue → API HTTP → serve frontend.
- [ ] **9.2** Scrivere test unitari per `internal/store` (operazioni CRUD, recovery).
- [ ] **9.3** Scrivere test unitari per `internal/queue` (enqueue, dequeue, concorrenza tra canali, limite 1 per canale).
- [ ] **9.4** Scrivere test unitari per `internal/searchagg` (aggregazione, timeout, filtri, paginazione).
- [ ] **9.5** Scrivere test unitari per `internal/ircmanager` (connect, reconnect, backoff).
- [ ] **9.6** Scrivere test di integrazione per le API REST (endpoint principali).
- [ ] **9.7** Verificare che i comandi CLI esistenti (`xdcc-dl`, `xdcc-search`, `xdcc-browse`) continuino a funzionare senza regressioni.

### Fase 10 — Dockerfile e Deploy

- [ ] **10.1** Aggiornare il `Dockerfile` per buildare anche `xdcc-server` e includerlo nell'immagine finale.
- [ ] **10.2** Configurare l'esposizione della porta HTTP nel Dockerfile (es. `EXPOSE 8080`).
- [ ] **10.3** Aggiungere volume per persistenza SQLite e directory download.
- [ ] **10.4** Documentare nel README la nuova modalità server: come avviarlo, come configurarlo, come accedere alla web UI.

---

## Punti di Investigazione Futuri

- [ ] **F.1** **AI Web Scraping**: Investigare la possibilità di usare intelligenza artificiale (LLM) per fare scraping di pagine web e individuare pacchetti XDCC da scaricare. Casi d'uso: siti che non hanno API strutturate, forum, pagine con liste di release. L'IA potrebbe parsare pagine HTML non strutturate e estrarre informazioni su bot, pack number e filename.

- [ ] **F.2** **Download Schedulati**: Investigare la possibilità di schedulare ricerche automatiche e download. Funzionalità desiderata:
  - Definire una "subscription" a una serie (es. "Nome Serie S01").
  - Configurare giorno della settimana e orario in cui effettuare la ricerca.
  - Se viene trovata una nuova puntata (non già scaricata), avviare automaticamente il download.
  - Logica di rilevamento "puntata successiva" (incremento numero episodio).
  - Notifiche (opzionali) quando una nuova puntata viene trovata e messa in download.

---

## Note Tecniche

- **Nessuna dipendenza da database pesanti**: si usa SQLite (CGO-free) o al massimo un file JSON. SQLite è preferito per la robustezza transazionale.
- **Frontend embedded**: il frontend è compilato dentro il binario Go per semplicità di distribuzione (singolo eseguibile).
- **Comandi CLI preservati**: `xdcc-dl`, `xdcc-search`, `xdcc-browse` restano invariati e continuano a funzionare indipendentemente dal server.
- **Comunicazione real-time**: SSE (Server-Sent Events) per aggiornamenti push unidirezionali (progresso download, cambi stato). REST per operazioni CRUD. SSE è preferito a WebSocket perché più semplice e non richiede configurazione speciale con reverse proxy.
- **Deploy semplificato**: un singolo binario Go serve API + frontend statico. Un solo container Docker, una sola porta. Nessun server web separato (no Apache, no Node).
- **PWA**: manifest + service worker per installabilità su dispositivi mobili. La UI deve funzionare offline per la visualizzazione dello stato (cache degli ultimi dati noti).

---

## Compatibilità ARM64 (Raspberry Pi 4)

Tutte le dipendenze sono **pure Go** (zero CGO) e compatibili con `linux/arm64`:

| Dipendenza | Tipo | ARM64 | Note |
|---|---|---|---|
| `modernc.org/sqlite` | C→Go transpile | ✅ Ufficiale | Supporto esplicito `linux/arm64` nella matrice piattaforme. ~1.5x più lento di CGO sqlite per insert, ma adeguato per il nostro use case (pochi record). |
| `github.com/go-chi/chi` | Pure Go | ✅ | Zero dipendenze esterne, solo stdlib. |
| `gopkg.in/yaml.v3` | Pure Go | ✅ | Usato ovunque nell'ecosistema Go su ARM (kubectl, helm, etc.) |
| `github.com/lrstanley/girc` | Pure Go | ✅ | Zero dipendenze esterne, solo stdlib. |
| `github.com/PuerkitoBio/goquery` | Pure Go | ✅ | Dipende da `x/net` che ha fallback pure Go. |
| `github.com/spf13/cobra` | Pure Go | ✅ | `mousetrap` è Windows-only (no-op su Linux). |

### SQLite: `modernc.org/sqlite` vs `ncruces/go-sqlite3`

| Criterio | modernc.org/sqlite | ncruces/go-sqlite3 |
|---|---|---|
| Meccanismo | C transpilato in Go (ccgo) | WASM→Go (wasm2go) |
| ARM64 CI | Matrice ufficiale, no CI nativo | ✅ CI nativo su `ubuntu-24.04-arm` |
| Performance read grandi | ~3x più lento di CGO | ~1.3x più lento di CGO |
| Performance insert | ~1.5x più lento di CGO | ~1.8x più lento di CGO |
| Go minimo | Go 1.22 ✅ | Go 1.25 ⚠️ (richiede upgrade) |
| Driver `database/sql` | `"sqlite"` via `_ "modernc.org/sqlite"` | `"sqlite3"` via `_ "github.com/ncruces/go-sqlite3/driver"` |
| Caveat | Pinning `modernc.org/libc` | Maggior uso memoria per connessione |

**Scelta: `modernc.org/sqlite`** — compatibile con Go 1.22 attuale, performance adeguata per il nostro schema (poche decine/centinaia di righe), nessun upgrade Go richiesto.

### Cross-compilation Docker

Il `Dockerfile` esistente è già corretto per multi-arch:
```dockerfile
CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build ...
```
`CGO_ENABLED=0` è la chiave: tutti i moduli compilano senza cross-compiler C. `docker buildx build --platform=linux/arm64` funziona senza modifiche.

### Attenzione dopo `go get modernc.org/sqlite`

Pinning obbligatorio di `modernc.org/libc`:
```bash
go get modernc.org/libc@$(go list -m -f '{{.Version}}' modernc.org/libc)
```
