# Piano di rientro - punti 1, 2, 3, 4, 6, 10

## Obiettivo
Allineare l'implementazione dei punti 1-3 eliminando i bug critici emersi in review, senza allargare lo scope ad altre feature.

## Ordine consigliato
1. Punto 2
2. Punto 1
3. Punto 3
4. Punto 4
5. Punto 6
6. Punto 10

> Nota: i punti 1 e 2 sono accoppiati; conviene risolverli nello stesso ciclo.

---

## [ ] Punto 2 - Gestione connessioni in `ConnectServer` (mappa `m.conns`)

### Problema
La connessione esistente viene rimossa dalla mappa troppo presto, con rischio di stato incoerente.

### Interventi
- [ ] Rifattorizzare `ConnectServer` per non fare `delete(m.conns, srv.ID)` prima delle decisioni.
- [ ] Se esiste una connessione gia `connected`, restituire `nil` senza alterare la mappa.
- [ ] Se esiste una connessione non valida/stale, cancellarla fuori dalla sezione critica e sostituirla in modo atomico.
- [ ] Evitare chiamate potenzialmente bloccanti mentre `m.mu` e lockato.

### Criteri di accettazione
- La connessione attiva resta sempre tracciata in `m.conns`.
- Nessun percorso lascia il manager senza riferimento a una connessione viva.

---

## [ ] Punto 1 - Loop di reconnect non funzionante

### Problema
Il loop `run()` termina quando vede `disconnected`, impedendo il reconnect automatico.

### Interventi
- [ ] Introdurre una distinzione esplicita tra:
  - disconnessione intenzionale (stop utente/server),
  - errore di connessione iniziale,
  - drop non intenzionale.
- [ ] Modificare `connect()` per restituire un esito strutturato (enum o bool + reason).
- [ ] In `run()`, avviare `reconnectBackoff()` solo per drop/errori non intenzionali.
- [ ] Mantenere il comportamento "5 tentativi esponenziali + ogni ora".

### Criteri di accettazione
- Se la rete cade, il reconnect parte sempre.
- Se l'utente disconnette esplicitamente, il reconnect non parte.

---

## [ ] Punto 3 - Stato DB `connected` impostato troppo presto

### Problema
Lo stato `connected` viene scritto prima che l'handshake IRC sia completato.

### Interventi
- [ ] Rimuovere `SetServerConnected()` dai punti pre-connessione.
- [ ] Aggiornare stato DB a `connected` solo nel callback `girc.CONNECTED`.
- [ ] Usare stati intermedi coerenti (`connecting`/`reconnecting`) durante il tentativo.

### Criteri di accettazione
- Nessun server risulta `connected` in DB prima dell'evento `CONNECTED`.

---

## [ ] Punto 4 - Evento `server_disconnected` non emesso

### Problema
L'evento e definito ma non viene pubblicato.

### Interventi
- [ ] Emettere `server_disconnected` su:
  - disconnect esplicito,
  - perdita connessione non intenzionale.
- [ ] Includere metadata minimi utili (`server_id`, `server_addr`, timestamp, motivo opzionale).
- [ ] Garantire coerenza tra stato DB e stream eventi.

### Criteri di accettazione
- Ogni transizione a stato disconnesso produce un evento osservabile.

---

## [ ] Punto 6 - Join canali non persistito correttamente

### Problema
Il join manuale puo non creare/aggiornare correttamente il record canale in DB.

### Interventi
- [ ] In `JoinChannel`, normalizzare sempre il nome canale.
- [ ] Se il canale non esiste nel DB, crearlo (`server_id`, `name`, `auto_join=true`).
- [ ] Se esiste, aggiornare `auto_join=true` e `joined=true` al join riuscito.
- [ ] In `LeaveChannel`, aggiornare `joined=false` e valutare `auto_join=false` per evitare rejoin automatico indesiderato.
- [ ] Aggiungere vincolo di unicita logica su `(server_id, name)` (migrazione dedicata) per evitare duplicati.

### Criteri di accettazione
- Un canale joinato manualmente compare sempre e in modo stabile nel DB.
- Nessun duplicato dello stesso canale sullo stesso server.

---

## [ ] Punto 10 - Backup DB con query costruita via stringa

### Problema
La query `VACUUM INTO` usa interpolazione stringa e non e robusta con path speciali.

### Interventi
- [ ] Eliminare interpolazione diretta non sanificata.
- [ ] Implementare escaping SQL sicuro del path oppure una strategia di backup alternativa robusta.
- [ ] Validare path di destinazione (assoluto, directory esistente/scrivibile, niente caratteri non supportati).
- [ ] Gestire errori con messaggi espliciti e non ambigui.

### Criteri di accettazione
- Backup funzionante anche con path contenenti caratteri speciali comuni.
- Nessuna esecuzione SQL non prevista da input path.

---

## Verifica finale (per ogni punto completato)
- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] Build server: `go build ./cmd/xdcc-server`
- [ ] Test manuale minimo del flusso toccato (connect/reconnect/eventi/join/backup).

