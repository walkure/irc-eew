package irc

import (
	"net"
	"strconv"

	goirc "github.com/fluffle/goirc/client"

	"github.com/walkure/irc-eew/internal/config"
)

// clientConfig converts one configured IRC server into a goirc client.Config.
func clientConfig(srv config.IRCServerConfig) *goirc.Config {
	// irc-eew.pl's IRCSock.pm sends "USER <name> 0 * :<desc>" — i.e. Perl's
	// "name" field is the IRC ident/username and "desc" is the realname
	// (gecos). goirc.NewConfig(nick, ident, name) mirrors that: its second
	// and third arguments become Config.Me.Ident and Config.Me.Name, which
	// client.Conn.User(ident, name) sends as "USER ident 12 * :name".
	cfg := goirc.NewConfig(srv.Nick, srv.Name, srv.Desc)
	cfg.Server = net.JoinHostPort(srv.Server.Host, strconv.Itoa(srv.Server.Port))
	cfg.Pass = srv.Server.Password
	// No flood control, matching irc-eew.pl (README.ja.md: "現時点では未対策").
	cfg.Flood = true
	return cfg
}
