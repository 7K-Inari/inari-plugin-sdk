package pluginsdk

import "github.com/hashicorp/go-plugin"

// ProtocolVersion is the negotiated plugin protocol version. Bump on breaking
// transport changes; hosts refuse plugins with a mismatched version.
const ProtocolVersion = 1

const (
	magicCookieKey   = "INARI_PLUGIN_MAGIC_COOKIE"
	magicCookieValue = "inari"
)

// Handshake returns the hashicorp go-plugin handshake configuration shared by
// every Inari plugin: magic cookie plus protocol version (§5.8).
func Handshake() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   magicCookieKey,
		MagicCookieValue: magicCookieValue,
	}
}
