package irc

import (
	"strings"
	"time"

	"github.com/lrstanley/girc"
)

func (c *Client) registerHandlers() {
	c.irc.Handlers.Add(girc.CONNECTED, func(client *girc.Client, e girc.Event) {
		c.connectTime = time.Now()
		c.infof("Connected to server")
		close(c.connectedCh)
	})

	// End of WHOIS: decide whether to send XDCC now or wait for JOIN.
	c.irc.Handlers.Add(girc.RPL_ENDOFWHOIS, func(client *girc.Client, e girc.Event) {
		c.debugf("End of WHOIS")
		if c.messageSent.Load() {
			return
		}
		if c.needsJoin.Load() {
			// We sent a JOIN; wait for the JOIN event to trigger XDCC.
			return
		}
		if c.whoisFoundChannels.Load() {
			// All channels were already joined — send XDCC directly.
			c.sendXDCCRequest(client)
			return
		}
		// No channels found in WHOIS at all.
		if c.opts.FallbackChannel != "" {
			ch := c.opts.FallbackChannel
			if !strings.HasPrefix(ch, "#") {
				ch = "#" + ch
			}
			c.debugf("No channels from WHOIS; joining fallback channel %s", ch)
			c.needsJoin.Store(true)
			client.Cmd.Join(ch)
		} else {
			c.debugf("No channels from WHOIS and no fallback; sending XDCC request directly")
			c.sendXDCCRequest(client)
		}
	})

	// WHOIS channels: join only channels we have not yet joined.
	c.irc.Handlers.Add(girc.RPL_WHOISCHANNELS, func(client *girc.Client, e girc.Event) {
		if len(e.Params) < 2 {
			return
		}
		c.logf("WHOIS channels: %s", e.Params[len(e.Params)-1])
		rawChannels := e.Params[len(e.Params)-1]
		for _, part := range strings.Fields(rawChannels) {
			part = strings.TrimLeft(part, "@+%&~")
			if !strings.HasPrefix(part, "#") {
				continue
			}
			ch := strings.ToLower(part)
			c.whoisFoundChannels.Store(true)
			c.mu.Lock()
			alreadyIn := c.joinedChannels[ch]
			c.mu.Unlock()
			if alreadyIn {
				c.logf("Already in channel %s, skipping JOIN", part)
			} else {
				c.logf("Joining channel %s", part)
				c.needsJoin.Store(true)
				time.Sleep(time.Duration(1+randN(2)) * time.Second)
				client.Cmd.Join(part)
			}
		}
	})

	// JOIN: record membership, send XDCC if pending.
	c.irc.Handlers.Add(girc.JOIN, func(client *girc.Client, e girc.Event) {
		if e.Source == nil || !strings.EqualFold(e.Source.Name, client.GetNick()) {
			return
		}
		ch := strings.ToLower(e.Params[0])
		c.mu.Lock()
		c.joinedChannels[ch] = true
		c.mu.Unlock()
		c.debugf("Joined channel: %s", e.Params[0])
		if !c.messageSent.Load() {
			c.sendXDCCRequest(client)
		}
	})

	// CTCP DCC handler (DCC SEND / DCC ACCEPT for resume).
	c.irc.CTCP.Set("DCC", func(client *girc.Client, ctcp girc.CTCPEvent) {
		sourceHost := ""
		if ctcp.Source != nil {
			sourceHost = ctcp.Source.Host
		}
		c.handleDCC(ctcp.Text, sourceHost)
	})

	// NOTICE from bot.
	// The handler distinguishes three classes of messages:
	//   1. Server ident/hostname checks — downgraded to logf (verbose only) because
	//      they are emitted by the server itself, not the bot, and clutter normal output.
	//   2. "Already requested" messages — trigger a 60 s wait + retry.
	//   3. "Denied / slot busy" messages — abort with ErrBotDenied.
	// Message patterns include both English and Italian strings because several
	// Rizon bots (particularly Italian ones) reply in Italian.
	c.irc.Handlers.Add(girc.NOTICE, func(client *girc.Client, e girc.Event) {
		notice := e.Last()
		msg := strings.ToLower(notice)
		// These are standard IRC server ident/hostname check messages — suppress in quiet mode.
		quietFiltered := []string{
			"looking up your hostname",
			"checking ident",
			"couldn't resolve your hostname",
			"no ident response",
		}
		isQuietFiltered := false
		for _, f := range quietFiltered {
			if strings.Contains(msg, f) {
				isQuietFiltered = true
				break
			}
		}
		if isQuietFiltered {
			c.logf("Bot notice: %s", notice)
		} else {
			c.noticef("Bot notice: %s", notice)
		}

		alreadyReqMsgs := []string{"you already requested", "richiesto questo pack!"}
		blockedMsgs := []string{"xdcc send negato", "numero pack errato", "invalid pack number",
			"gli slots sono occupati", "denied"}

		for _, s := range alreadyReqMsgs {
			if strings.Contains(msg, s) {
				c.finishWithNotice(ErrPackAlreadyReq, notice)
				return
			}
		}
		for _, s := range blockedMsgs {
			if strings.Contains(msg, s) {
				c.finishWithNotice(ErrBotDenied, notice)
				return
			}
		}
	})

	c.irc.Handlers.Add(girc.ERR_NOSUCHNICK, func(client *girc.Client, e girc.Event) {
		c.noticef("Bot '%s' not found on server", c.currentPack().Bot)
		c.finishWithError(ErrBotNotFound)
	})

	c.irc.Handlers.Add(girc.ERROR, func(client *girc.Client, e girc.Event) {
		c.noticef("IRC error: %s", e.Last())
		c.finishWithError(ErrUnrecoverable)
	})
}

func (c *Client) sendXDCCRequest(client *girc.Client) {
	if c.messageSent.Swap(true) {
		return
	}
	if c.opts.WaitTime > 0 {
		c.logf("Waiting %ds before sending XDCC request", c.opts.WaitTime)
		time.Sleep(time.Duration(c.opts.WaitTime) * time.Second)
	}
	pack := c.currentPack()
	msg := pack.GetRequestMessage(false)
	c.debugf("Sending XDCC request: /msg %s %s", pack.Bot, msg)
	client.Cmd.Message(pack.Bot, msg)
}
