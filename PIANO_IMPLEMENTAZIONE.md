# Piano di Implementazione — Modalità Client-Server XDCC-Go

## Panoramica

Aggiunta di una modalità client-server all'attuale tool CLI. Il server gestisce connessioni IRC persistenti, code di download e logica di retry. Il client è una web app responsive (PWA) che comunica col server via REST API.

---

## Architettura Generale

```
┌──────────────────────────────┐       ┌──────────────────────────────────┐
│         WEB CLIENT           │       │            SERVER                │
│  (SPA PWA - responsive)      │◄─────►│  REST API + SSE stream           │
│  HTML/CSS/JS/Svelte          |       |                                  |
|      (embedded)              │       |                                  │
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

- [ ] **1.1** Creare il file di configurazione `config.yaml` con struttura per: server IRC di default (con canali), directory di download (temp e destinazione), porta HTTP del server, timeout ricerca provider, page size di default, credenziali IRC (nickname base), livello log, path file log, policy conflitti file (`skip` di default), modalita' fallback download fallito (`suggest_only` default), limiti banda (`downloads.max_rate_bps`) e fasce orarie, provider ricerca abilitati/disabilitati, stato setup guidato (`ui.setup_completed`).
- [ ] **1.2** Creare il package `internal/config` che carica e valida la configurazione da file YAML + variabili d'ambiente + flag CLI. l'ordine di priorità deve essere flag cli poi variabili di ambiente ed infine viene il file YAML.
- [ ] **1.3** Creare la struttura del comando `cmd/xdcc-server/main.go` con cobra, che avvia il server HTTP e il gestore connessioni IRC. Deve accettare flag `--config`, `--port`, `--download-dir`, `--temp-dir`.
- [ ] **1.4** Aggiornare `go.mod` con le nuove dipendenze: un router HTTP: `chi` , `go-yaml`, `modernc.org/sqlite` (CGO-free). Nota: SSE non richiede librerie aggiuntive, si implementa con la stdlib.

### Fase 2 — Persistenza (SQLite)

- [ ] **2.1** Creare il package `internal/store` con interfaccia per la persistenza. Usare SQLite (CGO-free con `modernc.org/sqlite`) come backend.
- [ ] **2.2** Definire lo schema del database:
  - Tabella `irc_servers`: id, address, port, auto_connect (bool), status, last_connected_at, retry_count.
  - Tabella `irc_channels`: id, server_id (FK), name, auto_join (bool), topic, joined (bool).
  - Tabella `downloads`: id, pack_message, bot, server_address, channel, filename, filesize, status (queued/downloading/completed/failed/paused), progress_bytes, speed_bps, created_at, started_at, completed_at, error_message, priority/position.
  - Tabella `search_cache`: query_key, provider, payload_json, fetched_at, expires_at, stale_expires_at.
  - Tabella `schema_version`: version, applied_at.
  - Tabella `search_presets`: id, name, query, filters_json, is_default, created_at, updated_at.
  - Tabella `watchlists`: id, name, query, filters_json, enabled, auto_enqueue, last_checked_at, last_match_fingerprint, last_notified_at.
  - Tabella `provider_stats`: provider, window_start, window_end, requests, successes, timeouts, failures, avg_latency_ms, updated_at.
- [ ] **2.3** Implementare le operazioni CRUD per servers, channels e downloads. Metodi: `EnqueueDownload`, `GetQueue(channel)`, `UpdateProgress`, `MarkCompleted`, `MarkFailed`, `GetActiveDownloads`, `GetPendingByChannel`, `RecoverOnStartup`, `GetDownloadHistory(page, pageSize)`.
- [ ] **2.4** Implementare la logica di recovery: all'avvio del server, i download con status `downloading` vengono rimessi in coda come `queued` per essere ritentati.
- [ ] **2.5** Implementare lo svecchiamento periodico del database:
  - **Downloads completati/falliti**: eliminati dopo un TTL configurabile (default 30 giorni). Configurazione: `config.yaml` → `storage.downloads_retention: 30d`. I download con status `queued`/`downloading` non vengono mai eliminati.
  - **Cache ricerca**: eliminazione entry oltre il TTL stale (24h) — già gestito dalla goroutine di pulizia del search cache (punto 5.6).
  - **Goroutine background**: eseguire la pulizia ogni 12 ore (intervallo configurabile). `DELETE FROM downloads WHERE status IN ('completed','failed') AND completed_at < datetime('now', '-30 days')`.
  - **VACUUM**: 1 volta alla settimana eseguire `VACUUM` per recuperare spazio disco o su richiesta via API.
- [ ] **2.6** Implementare migrazioni SQLite versionate:
  - Runner migrazioni all'avvio con tabella `schema_version`.
  - Migrazioni incrementali e idempotenti.
  - Backup automatico del DB prima di migrazioni distruttive.
  - Errore esplicito e stop dell'avvio se una migrazione fallisce.
- [ ] **2.7** Implementare riconciliazione DB <-> filesystem all'avvio:
  - Se esistono file temporanei parziali e relativo record `downloads`, riaccodarli per resume.
  - Se esistono record `downloading` ma il file temporaneo manca, riportarli a `queued`.
  - Se esistono file temporanei orfani senza record DB, eliminarli o spostarli in una cartella `orphaned/` secondo policy configurabile.
- [ ] **2.8** Implementare backup/export/import di configurazione e stato:
  - Export di `config.yaml` + subset SQLite rilevante (server, canali, queue, history, cache opzionale).
  - Import controllato con validazione della versione schema.
  - Snapshot/backup del DB prima di upgrade o operazioni distruttive.

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
- [ ] **4.3** Prevedere un limite globale ai download paralleli: oltre al vincolo `1 download per canale`, introdurre `downloads.max_parallel_total` configurabile (default: 5). I job eccedenti restano in coda anche se il loro canale e' libero.
- [ ] **4.4** Implementare `onDownloadComplete(channel)`: quando un download finisce (successo o fallimento), prende il prossimo dalla coda dello stesso canale e lo avvia rispettando il limite globale del numero massimo di download paralleli. Prevedere un job che monitora i download attivi e avvia nuovi job dalla coda quando si liberano slot disponibili, schedulato ogni 10 secondi (configurabile), la priorità all'avvio del download è data dall'ordine di enqueue (FIFO) ma con rispetto del vincolo 1 per canale e del limite globale.
- [ ] **4.5** Integrare con il client IRC esistente (`internal/irc`) per eseguire il download effettivo. Riusare la logica di `DownloadAll` adattandola per operare su singoli pack con reporting del progresso via callback.
- [ ] **4.6** Implementare il reporting del progresso in tempo reale: bytes scaricati, velocità, ETA per aggiornare il client via SSE.
- [ ] **4.7** Implementare la persistenza della coda: ogni cambio di stato (enqueue, start, progress, complete, fail) viene scritto nel DB SQLite.
- [ ] **4.8** Implementare il recovery all'avvio: leggere dal DB i download incompleti e rimetterli in coda.
- [ ] **4.9** Supportare le directory configurabili: temp dir per file in corso di download, destination dir per file completati. Spostare il file da temp a destination al completamento.
- [ ] **4.10** Definire la policy sui conflitti file finali: se il file di destinazione esiste gia', comportamento di default `skip` con stato esplicito (`skipped_existing`). La policy deve essere coerente tra UI, API e CLI delegata.
- [ ] **4.11** Implementare fallback intelligente su download fallito:
  - Quando un download fallisce, cercare alternative compatibili (filename/size simile) su provider/bot diversi.
  - Modalita' configurabile: `suggest_only` (default) o `auto_retry_best`.
  - Tracciare nel DB il legame tra job originale e fallback proposto/usato.
- [ ] **4.12** Implementare limitazione banda e fasce orarie:
  - Throttle globale e/o per singolo download.
  - Profilo orario (quiet hours) applicato senza interrompere in modo distruttivo i download in corso.
- [ ] **4.13** Esporre operazioni bulk sul queue manager:
  - `pause/resume/remove` per lista ID o per canale.
  - Risultato per-item (success/fail/skipped) per feedback chiaro lato UI.

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
- [ ] **5.6** Implementare cache dei risultati di ricerca (solo server, non CLI standalone):
  - **Storage**: in-memory con persistenza opzionale su SQLite (abilitabile tramite configurazione di default spento) per il fallback stale (i risultati "freschi" stanno in RAM, quelli stale vengono scritti anche su DB per sopravvivere a un restart).
  - **Chiave di cache**: query normalizzata (lowercase + trim). Ogni entry memorizza i risultati grezzi (pre-filtro) + timestamp + provider di origine.
  - **TTL fresco**: 30 minuti di default (configurabile in `config.yaml`). Se la cache è fresca, si ritorna direttamente senza contattare i provider.
  - **TTL stale / fallback**: 24 ore. Se la cache è scaduta (>30min) si tenta la ricerca live; se i provider non rispondono o vanno in timeout, si ritorna la cache stale con un header/flag che indica "risultati da cache (provider non disponibili)".
  - **Filtri**: sempre applicati DOPO il recupero dalla cache (la cache contiene risultati grezzi). Questo massimizza la riusabilità: una singola entry in cache serve query con filtri diversi.
  - **Invalidazione**: pulizia periodica (goroutine background) delle entry oltre il TTL stale (24h). Le entry oltre le 24h vengono eliminate.
  - **Configurazione**: `config.yaml` → `search.cache.fresh_ttl: 30m`, `search.cache.stale_ttl: 24h`, `search.cache.enabled: true`.
- [ ] **5.7** Esporre provenance e stato dei provider nella ricerca:
  - Per ogni risposta API indicare se i risultati arrivano da `live`, `cache_fresh` o `cache_stale`.
  - Includere lo stato per provider (`ok`, `timeout`, `failed`, `skipped_cache_hit`).
  - Restituire metadati utili a UI e CLI delegata: eta' della cache, numero provider rispondenti, warning su risultati parziali.
- [ ] **5.8** Implementare preset ricerca riusabili:
  - Salvataggio preset (nome + query + filtri) e applicazione rapida.
  - Possibilita' di marcare preset preferiti/default.
- [ ] **5.9** Implementare watchlist con rilevamento novita':
  - Esecuzione manuale (`run now`) e periodica configurabile.
  - Confronto fingerprint risultati rispetto all'ultimo run per individuare nuove entry.
  - Opzione `auto_enqueue` per accodare automaticamente i nuovi match.
- [ ] **5.10** Implementare provider insights:
  - Raccolta metriche per provider (latenza media, timeout rate, success rate).
  - Supporto enable/disable provider a runtime (senza riavvio server).

### Fase 6 — REST API

- [ ] **6.1** Creare il package `internal/api` con router HTTP. Endpoint:
  - `GET /healthz` — processo vivo.
  - `GET /readyz` — dipendenze minime pronte (DB ok, config valida, sottosistemi avviati).
  - `GET /api/version` — versione server e versione minima compatibile per i client CLI.
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
  - `GET /api/downloads/history?page=...&pageSize=...` — cronologia download completati/falliti (paginata).
  - `GET /api/downloads/:id` — ottieni stato e progresso di un singolo download.
  - `DELETE /api/downloads/:id` — cancella/rimuovi download dalla coda.
  - `POST /api/downloads/:id/pause` — mette in pausa un download o una entry in coda.
  - `POST /api/downloads/:id/resume` — riprende un download pausato.
  - `POST /api/downloads/:id/retry` — rimette in coda un download fallito.
  - `GET /api/config` — configurazione corrente.
  - `PUT /api/config` — aggiorna la configurazione runtime (scrive su `config.yaml` con backup).
  - `GET /api/stats` — statistiche: totale scaricato, conteggi, velocità media, uptime, spazio disco.
  - `GET /api/status` — stato operativo sintetico: connessioni IRC, coda, spazio disco, eventuali warning attivi.
  - `POST /api/admin/export` — esporta configurazione e stato.
  - `POST /api/admin/import` — importa configurazione e stato da backup compatibile.
  - `PATCH /api/downloads/:id/position` — riordinamento priorità download in coda.
  - `POST /api/downloads/bulk` — azioni bulk su download/coda (`pause`, `resume`, `remove`) con risposta per-item.
  - `GET /api/search/presets` / `POST /api/search/presets` / `PUT /api/search/presets/:id` / `DELETE /api/search/presets/:id` — gestione preset ricerca.
  - `GET /api/watchlists` / `POST /api/watchlists` / `PUT /api/watchlists/:id` / `DELETE /api/watchlists/:id` — gestione watchlist.
  - `POST /api/watchlists/:id/run` — esecuzione immediata di una watchlist.
  - `GET /api/search/providers` — stato e metriche provider.
  - `PATCH /api/search/providers/:name` — enable/disable provider a runtime.
  - `POST /api/xdcc/parse` — parse di stringhe XDCC raw (`/msg Bot XDCC SEND #123`) in payload download.
  - `GET /api/setup/status` / `POST /api/setup/bootstrap` — stato setup guidato e bootstrap iniziale.
- [ ] **6.2** Implementare middleware: CORS, logging, error handling standardizzato (JSON errors), request ID e logging strutturato. Definire una struttura di errore standard (es. `{"error": {"code": "...", "message": "...", "request_id": "..."}}`) per garantire una gestione coerente lato client.
- [ ] **6.3** Servire i file statici della web app dalla stessa porta HTTP (embedded nel binario con `embed`).

### Fase 7 — SSE (Server-Sent Events) per Aggiornamenti Real-Time

- [ ] **7.1** Implementare endpoint SSE `GET /api/events` che invia eventi in tempo reale ai client connessi. Usa `Content-Type: text/event-stream`. Nessuna configurazione speciale necessaria con reverse proxy.
- [ ] **7.2** Definire i tipi di evento (campo `event:` nel protocollo SSE):
  - `server_status_changed` (connected/disconnected/reconnecting)
  - `channel_joined` / `channel_left` / `channel_topic_updated`
  - `download_started` / `download_progress` / `download_completed` / `download_failed`
  - `download_queued` / `download_removed`
  - `download_alternative_found` / `download_bulk_action_result`
  - `watchlist_new_results`
  - `provider_health_changed`
- [ ] **7.3** Implementare hub di broadcast: gestione connessioni SSE multiple (ogni client ha una goroutine dedicata), fan-out degli eventi a tutti i client connessi. Gestione graceful close quando il client si disconnette.
- [ ] **7.4** Per `download_progress`, inviare aggiornamenti a intervalli regolari (es. ogni 500ms) con: bytes scaricati, filesize, velocità, ETA.
- [ ] **7.5** Rendere SSE piu' robusti in caso di reconnessione:
  - Attribuire un `event id` progressivo agli eventi e mantenere un buffer degli ultimi N eventi (es. 100) in memoria sul server.
  - Supportare l'header `Last-Event-ID`: se un client si riconnette e il suo ID è nel buffer, il server invia solo gli eventi mancanti.
  - Se l'ID è troppo vecchio (non più nel buffer), il server invia un evento speciale `resync_required` che istruisce il client a ricaricare lo stato completo via API prima di riprendere lo stream.

### Fase 8 — Web App (Frontend PWA)

- [ ] **8.1** Creare la struttura del frontend in `web/` utilizzando **Svelte/SvelteKit**. Questo garantirà una UI reattiva, manutenibile e performante. Il frontend verrà compilato e i suoi asset statici verranno incorporati nel binario Go con `go:embed`.
- [ ] **8.2** Implementare la pagina principale: lista dei server connessi con indicatore di stato (verde/rosso/giallo per connesso/disconnesso/reconnecting).
- [ ] **8.3** Implementare la vista server: cliccando un server si mostra la lista canali joinati, con possibilità di joinare nuovi canali o lasciare quelli esistenti.
- [ ] **8.4** Implementare la vista canale: cliccando un canale si mostra il topic del canale.
- [ ] **8.5** Implementare la pagina di ricerca: campo di ricerca + filtri (prefix, bot, extension, compact). Risultati paginati (default 50 per pagina). Ogni risultato è cliccabile.
- [ ] **8.6** Implementare l'azione click su risultato di ricerca: cliccando un risultato si avvia/accoda il download del pacchetto. Feedback visivo immediato (toast/snackbar).
- [ ] **8.7** Implementare la pagina downloads: sezione download in corso (con barra di avanzamento, velocità, ETA, filename, dimensione) + sezione download in coda (ordinati per canale e posizione) + sezione cronologia (download completati/falliti, paginata, con data, dimensione, stato).
- [ ] **8.8** Implementare la connessione SSE (`EventSource`) per ricevere aggiornamenti real-time. Aggiornare la UI senza refresh della pagina. Riconnessione automatica gestita nativamente dal browser.
- [ ] **8.9** Rendere la UI responsive: layout mobile-first con breakpoints per tablet e desktop. Menu hamburger su mobile, sidebar su desktop.
- [ ] **8.10** Implementare il `manifest.json` e service worker per rendere la web app installabile come PWA (Add to Home Screen su Android/iOS).
- [ ] **8.11** Aggiungere la pagina impostazioni: configurazione directory temp/destinazione, timeout ricerca, page size, gestione server di default, retention download (giorni), fallback mode download fallito, limiti banda/fasce orarie, provider abilitati.
- [ ] **8.12** Implementare **dark mode**: toggle light/dark nella UI (salvare preferenza in localStorage). Default: segue preferenza di sistema (`prefers-color-scheme`). Usare CSS custom properties per gestire i temi senza duplicare stili.
- [ ] **8.13** Implementare **dashboard / statistiche**: pagina con:
  - Totale scaricato (oggi, questa settimana, questo mese) in bytes/GB.
  - Numero download completati / falliti / in coda.
  - Velocità media download.
  - Uptime del server.
  - Spazio disco disponibile nella directory download.
  - Stato connessioni IRC (quanti server connessi / totali).
- [ ] **8.14** Aggiungere controlli manuali sui download nella UI:
  - Pulsante `pause` / `resume`.
  - Pulsante `retry` per download falliti.
  - Messaggi chiari quando un download e' duplicato, pausato per spazio disco o ripristinato dopo un restart.
- [ ] **8.15** Mostrare provenance e warning della ricerca nella UI:
  - Badge `live` / `cache fresh` / `cache stale`.
  - Stato dei provider (ok / timeout / failed).
  - Messaggio esplicito quando il risultato e' parziale o servito da fallback cache.
- [ ] **8.16** Aggiungere azioni bulk in ricerca e coda:
  - Multi-select nella ricerca con `enqueue selezionati`.
  - Azioni batch nella coda (`pause/resume/remove`) su selezione o su intero canale.
- [ ] **8.17** Implementare pagina watchlist:
  - CRUD watchlist, pulsante `run now`, evidenza nuovi risultati.
  - Azione rapida `accoda nuovi` o `auto_enqueue`.
- [ ] **8.18** Implementare preset ricerca in UI:
  - Salvataggio/applicazione preset in un tap.
  - Preset favoriti visibili in alto nella pagina ricerca.
- [ ] **8.19** Implementare pannello provider insights:
  - Stato provider in tempo reale (latenza, timeout, failure).
  - Toggle enable/disable provider.
- [ ] **8.20** Implementare quick-add da stringa XDCC:
  - Campo `incolla comando` (es. `/msg Bot XDCC SEND #123`).
  - Chiedere server e canale tramite combo box, nella combo dei server mostrare i server a cui si è connessi nella combo dei canali mostrare i canali joinati su quel server.
  - Parser server-side e proposta conferma prima dell'enqueue.
- [ ] **8.21** Implementare setup wizard primo avvio:
  - Step guidati per directory, server default, nickname, test connessione, test scrittura.
  - Al completamento impostare `ui.setup_completed=true`.

### Fase 9 — Robustezza e Funzionalità Trasversali

- [ ] **9.1** **Graceful shutdown**: gestire SIGTERM/SIGINT nel server. Al ricevimento del segnale:
  - non avviare o accodare altri download
  - Salvare lo stato di tutti i download in corso nel DB (progress_bytes per il resume).
  - Chiudere le connessioni IRC con messaggio QUIT.
  - Chiudere le connessioni SSE aperte verso i client.
  - Timeout massimo di shutdown (es. 15s): se non tutto si chiude in tempo, force-kill.
- [ ] **9.2** **Controllo spazio disco**: prima di avviare un download, verificare lo spazio disponibile nella temp dir. Soglia minima configurabile (default: 1 GB). Se sotto soglia, mettere in pausa la coda e inviare evento SSE `disk_space_low`. Riprendere automaticamente quando lo spazio torna sopra la soglia.
- [ ] **9.3** **Gestione duplicati download**: al momento dell'enqueue, verificare:
  - Se un download identico (stesso bot + pack number + server) è già in coda o in corso → rifiutare con errore specifico.
  - Se un file con lo stesso nome è già stato scaricato (completato) → restituire warning nella risposta (il frontend può chiedere conferma all'utente).
- [ ] **9.4** **Riordinamento coda**: aggiungere endpoint `PATCH /api/downloads/:id/position` per cambiare la priorità di un download in coda. Nella UI implementare drag-and-drop (o frecce su/giù su mobile) per riordinare visivamente.
- [ ] **9.5** **Notifiche browser**: usare la Web Notification API per notificare l'utente quando:
  - Un download viene completato con successo.
  - Un download fallisce definitivamente (dopo tutti i retry).
  - Lo spazio disco è insufficiente.
  Richiedere permesso notifiche al primo accesso. Funziona anche con la PWA installata su Android.
- [ ] **9.6** **Logging strutturato e diagnostica**:
  - Log JSON o key/value su file con livello configurabile (`debug`, `info`, `warn`, `error`).
  - Rotazione log semplice o tramite documentazione esterna.
  - Eventi diagnostici principali: reconnect IRC, timeout provider, pause per low disk, retry download, recovery startup.
- [ ] **9.7** **Guardrail fallback intelligente**:
  - Soglia minima di similarita' per proporre alternative (evitare match errati).
  - Limite tentativi fallback per download (evitare loop infiniti).
  - Tracciamento chiaro del motivo di fallback/rifiuto lato API/UI.
- [ ] **9.8** **Gestione affidabile watchlist/alert**:
  - Cooldown notifiche per evitare spam su risultati invariati.
  - Deduplica notifiche su restart o riconnessione client.

### Fase 10 — Integrazione e Testing

- [ ] **10.1** Integrare tutti i componenti nel `cmd/xdcc-server/main.go`: avvio config → SQLite → IRC manager → download queue → API HTTP → serve frontend → graceful shutdown handler.
- [ ] **10.2** Scrivere test unitari per `internal/store` (operazioni CRUD, recovery, cleanup).
- [ ] **10.3** Scrivere test unitari per `internal/queue` (enqueue, dequeue, concorrenza tra canali, limite 1 per canale, duplicati, riordinamento).
- [ ] **10.4** Scrivere test unitari per `internal/searchagg` (aggregazione, timeout, filtri, paginazione, cache).
- [ ] **10.5** Scrivere test unitari per `internal/ircmanager` (connect, reconnect, backoff).
- [ ] **10.6** Scrivere test di integrazione per le API REST (endpoint principali).
- [ ] **10.7** Verificare compatibilita' client/server:
  - test su `/api/version`
  - errore chiaro se CLI e server hanno versioni incompatibili
  - test dei metadati provenance nella ricerca
- [ ] **10.8** Verificare che i comandi CLI esistenti (`xdcc-dl`, `xdcc-search`, `xdcc-browse`) continuino a funzionare senza regressioni.
- [ ] **10.9** Aggiungere test per le nuove feature utente:
  - Bulk actions (`/api/downloads/bulk`) e risultati per-item.
  - Preset/watchlist (run now, dedup novita', auto_enqueue).
  - Fallback intelligente (mode `suggest_only` default e `auto_retry_best`).
  - Provider insights e toggle runtime.
  - Parse quick-add da stringa XDCC.
- [ ] **10.10** Scrivere test End-to-End (E2E) per i flussi utente critici. Utilizzando un framework come **Playwright** o **Cypress**, simulare azioni nella Web UI (es. ricerca, avvio download, verifica progresso) per validare l'integrazione completa tra frontend e backend.

### Fase 11 — Delegazione CLI → Server (`--command-server`)

- [ ] **11.1** Aggiungere il flag `--command-server` a `xdcc-dl` e `xdcc-browse`. Il valore è il base URL del server (es. `--command-server=http://localhost:8080`). Se presente, il download viene delegato al server via REST API invece di aprire una connessione IRC standalone.
- [ ] **11.2** Implementare in `internal/cli` (o nuovo package `internal/client`) un thin HTTP client che:
  - Verifica la compatibilita' col server tramite `GET /api/version` prima di usare `--command-server`.
  - Invia `POST /api/downloads` con le informazioni del pack (bot, pack number, server, filename, directory di output).
  - Per `xdcc-browse`: prima fa la ricerca via `GET /api/search`, mostra i risultati con la stessa UI interattiva, poi invia i pack selezionati al server.
- [ ] **11.3** Implementare il feedback da terminale quando si delega al server (Approccio V1): dopo aver ricevuto il download ID, la CLI esegue polling sull'endpoint `GET /api/downloads/:id` a intervalli regolari (es. 1s) per recuperare e stampare barra di avanzamento, velocità ed ETA — stessa UX del download standalone. L'uso di SSE può essere un miglioramento futuro.
- [ ] **11.4** Gestire il caso di server non raggiungibile: se `--command-server` è specificato ma il server non risponde, restituire un errore chiaro (non fallback silenzioso a standalone, perché l'utente ha scelto esplicitamente di delegare).
- [ ] **11.5** Per `xdcc-browse` con `--command-server`: **ricerca e download passano sempre dal server**. Nessun fallback locale silenzioso. Se il server non e' raggiungibile o restituisce errore, il comando termina con errore esplicito.

### Fase 12 — Dockerfile e Deploy

- [ ] **12.1** Aggiornare il `Dockerfile` per buildare anche `xdcc-server` e includerlo nell'immagine finale.
- [ ] **12.2** Configurare l'esposizione della porta HTTP nel Dockerfile (es. `EXPOSE 8080`).
- [ ] **12.3** Aggiungere volume per persistenza SQLite e directory download.
- [ ] **12.4** Documentare nel README la nuova modalità server: come avviarlo, come configurarlo, come accedere alla web UI.
- [ ] **12.5** Documentare nel README il flag `--command-server` per i comandi CLI e il workflow di delega.
- [ ] **12.6** Aggiungere supporto operativo per Raspberry senza Docker:
  - unit file `systemd` di esempio
  - documentazione per auto-start al boot
  - restart policy consigliata e percorso dei log

---

## Punti di Investigazione Futuri

- [ ] **F.1** **AI Web Scraping**: Investigare la possibilità di usare intelligenza artificiale (LLM) per fare scraping di pagine web e individuare pacchetti XDCC da scaricare. Casi d'uso: siti che non hanno API strutturate, forum, pagine con liste di release. L'IA potrebbe parsare pagine HTML non strutturate e estrarre informazioni su bot, pack number e filename.

- [ ] **F.2** **Download Schedulati**: Investigare la possibilità di schedulare ricerche automatiche e download. Funzionalità desiderata:
  - Definire una "subscription" a una serie (es. "Nome Serie S01").
  - Configurare giorno della settimana e orario in cui effettuare la ricerca.
  - Se viene trovata una nuova puntata (non già scaricata), avviare automaticamente il download.
  - Logica di rilevamento "puntata successiva" (incremento numero episodio).
  - Notifiche (opzionali) quando una nuova puntata viene trovata e messa in download.
  - Estensione consigliata: riusare preset/watchlist (fase 5.8/5.9) come base di scheduling.

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
