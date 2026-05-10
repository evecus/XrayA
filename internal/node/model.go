package node

import "encoding/json"

// Protocol constants
const (
	ProtoVMess      = "vmess"
	ProtoVLESS      = "vless"
	ProtoTrojan     = "trojan"
	ProtoShadowsocks = "shadowsocks"
	ProtoHysteria2  = "hysteria2"
	ProtoSocks5     = "socks5"
	ProtoHTTP       = "http"
)

// Node is the universal node struct stored in DB.
type Node struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Address  string          `json:"address"`
	Port     int             `json:"port"`
	Link     string          `json:"link"`   // original share link
	GroupID  string          `json:"groupId"` // subscription group id, empty = manual
	Extra    json.RawMessage `json:"extra,omitempty"` // protocol-specific fields
}

// --- Protocol-specific Extra structs ---

type ExtraVMess struct {
	UUID     string `json:"uuid"`
	AlterId  int    `json:"alterId"`
	Security string `json:"security"` // auto/aes-128-gcm/chacha20-poly1305/none
	Network  string `json:"network"`  // tcp/ws/grpc/h2/quic
	TLS      bool   `json:"tls"`
	SNI      string `json:"sni"`
	Path     string `json:"path"`
	Host     string `json:"host"`
	GrpcSvc  string `json:"grpcSvc"`
	Flow     string `json:"flow"`
	Fp       string `json:"fp"` // fingerprint
}

type ExtraVLESS struct {
	UUID        string `json:"uuid"`
	Flow        string `json:"flow"`
	Encryption  string `json:"encryption"`
	Network     string `json:"network"`
	TLS         string `json:"tls"` // tls/reality/none
	SNI         string `json:"sni"`
	Fp          string `json:"fp"`
	PbKey       string `json:"pbKey"`  // reality public key
	ShortID     string `json:"shortId"` // reality short id
	Path        string `json:"path"`
	Host        string `json:"host"`
	GrpcSvc     string `json:"grpcSvc"`
	Insecure    bool   `json:"insecure"`
}

type ExtraTrojan struct {
	Password string `json:"password"`
	SNI      string `json:"sni"`
	Network  string `json:"network"`
	Path     string `json:"path"`
	Host     string `json:"host"`
	GrpcSvc  string `json:"grpcSvc"`
	Insecure bool   `json:"insecure"`
}

type ExtraShadowsocks struct {
	Method   string `json:"method"`
	Password string `json:"password"`
	Plugin   string `json:"plugin"`
	PluginOpt string `json:"pluginOpt"`
}

type ExtraHysteria2 struct {
	Password  string `json:"password"`
	SNI       string `json:"sni"`
	Insecure  bool   `json:"insecure"`
	Obfs      string `json:"obfs"`
	ObfsParam string `json:"obfsParam"`
	PinSHA256 string `json:"pinSHA256"`
}

type ExtraSocks5 struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
