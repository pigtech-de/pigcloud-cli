package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

type AgentInfo struct {
	Port    int       `json:"port"`
	Token   string    `json:"token"`
	PID     int       `json:"pid"`
	Expires time.Time `json:"expires"`
}

type KeyMaterial struct {
	PublicKey      [32]byte
	PrivateKey     [32]byte
	KyberPublicKey []byte
	KyberSeed      []byte
	NameKey        []byte

	SigningPublicKeyEd25519  []byte
	SigningPrivateKeyEd25519 []byte
	SigningPublicKeyMldsa    []byte
	SigningPrivateKeyMldsa   []byte
}

type Request struct {
	Token  string `json:"token"`
	Action string `json:"action"`
}

type Response struct {
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
	PublicKey      string `json:"public_key,omitempty"`
	PrivateKey     string `json:"private_key,omitempty"`
	KyberPublicKey string `json:"kyber_public_key,omitempty"`
	KyberSeed      string `json:"kyber_seed,omitempty"`
	NameKey        string `json:"name_key,omitempty"`

	SigningPublicKeyEd25519  string `json:"signing_public_key_ed25519,omitempty"`
	SigningPrivateKeyEd25519 string `json:"signing_private_key_ed25519,omitempty"`
	SigningPublicKeyMldsa    string `json:"signing_public_key_mldsa,omitempty"`
	SigningPrivateKeyMldsa   string `json:"signing_private_key_mldsa,omitempty"`
}

func agentFilePath() string {
	var configDir string
	if runtime.GOOS == "windows" {
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	} else {
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			home, _ := os.UserHomeDir()
			configDir = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(configDir, "pigcloud", "agent.json")
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeAgentFile(info *AgentInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	path := agentFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	return os.WriteFile(path, data, 0600)
}

func ReadAgentFile() *AgentInfo {
	data, err := os.ReadFile(agentFilePath())
	if err != nil {
		return nil
	}
	var info AgentInfo
	if json.Unmarshal(data, &info) != nil {
		return nil
	}
	if !info.Expires.IsZero() && time.Now().After(info.Expires) {
		RemoveAgentFile()
		return nil
	}
	return &info
}

func RemoveAgentFile() {
	os.Remove(agentFilePath())
}

func Serve(keys *KeyMaterial, ttl time.Duration) (port int, token string, err error) {
	token, err = generateToken()
	if err != nil {
		return 0, "", fmt.Errorf("generate token: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, "", fmt.Errorf("listen: %w", err)
	}
	port = listener.Addr().(*net.TCPAddr).Port

	expires := time.Now().Add(ttl)

	info := &AgentInfo{
		Port:    port,
		Token:   token,
		PID:     os.Getpid(),
		Expires: expires,
	}
	if err := writeAgentFile(info); err != nil {
		listener.Close()
		return 0, "", fmt.Errorf("write agent file: %w", err)
	}

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			for i := range keys.PrivateKey {
				keys.PrivateKey[i] = 0
			}
			for i := range keys.KyberSeed {
				keys.KyberSeed[i] = 0
			}
			for i := range keys.NameKey {
				keys.NameKey[i] = 0
			}
			for i := range keys.SigningPrivateKeyEd25519 {
				keys.SigningPrivateKeyEd25519[i] = 0
			}
			for i := range keys.SigningPrivateKeyMldsa {
				keys.SigningPrivateKeyMldsa[i] = 0
			}
			listener.Close()
			RemoveAgentFile()
		})
	}

	timer := time.AfterFunc(ttl, func() {
		shutdown()
	})

	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go handleConn(conn, keys, token, timer, shutdown)
	}

	shutdown()
	return port, token, nil
}

func handleConn(conn net.Conn, keys *KeyMaterial, token string, timer *time.Timer, shutdown func()) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req Request
	if err := dec.Decode(&req); err != nil {
		enc.Encode(Response{Error: "invalid request"})
		return
	}

	if req.Token != token {
		enc.Encode(Response{Error: "unauthorized"})
		return
	}

	switch req.Action {
	case "ping":
		enc.Encode(Response{OK: true})

	case "keys":
		enc.Encode(Response{
			OK:                       true,
			PublicKey:                hex.EncodeToString(keys.PublicKey[:]),
			PrivateKey:               hex.EncodeToString(keys.PrivateKey[:]),
			KyberPublicKey:           hex.EncodeToString(keys.KyberPublicKey),
			KyberSeed:                hex.EncodeToString(keys.KyberSeed),
			NameKey:                  hex.EncodeToString(keys.NameKey),
			SigningPublicKeyEd25519:  hex.EncodeToString(keys.SigningPublicKeyEd25519),
			SigningPrivateKeyEd25519: hex.EncodeToString(keys.SigningPrivateKeyEd25519),
			SigningPublicKeyMldsa:    hex.EncodeToString(keys.SigningPublicKeyMldsa),
			SigningPrivateKeyMldsa:   hex.EncodeToString(keys.SigningPrivateKeyMldsa),
		})

	case "shutdown":
		enc.Encode(Response{OK: true})
		timer.Stop()
		go func() {
			time.Sleep(100 * time.Millisecond)
			shutdown()
		}()

	default:
		enc.Encode(Response{Error: "unknown action"})
	}
}

func RequestKeys() *KeyMaterial {
	info := ReadAgentFile()
	if info == nil {
		return nil
	}

	resp, err := sendRequest(info, "keys")
	if err != nil || !resp.OK {
		return nil
	}

	pubBytes, err := hex.DecodeString(resp.PublicKey)
	if err != nil || len(pubBytes) != 32 {
		return nil
	}
	privBytes, err := hex.DecodeString(resp.PrivateKey)
	if err != nil || len(privBytes) != 32 {
		return nil
	}
	kyberPubBytes, err := hex.DecodeString(resp.KyberPublicKey)
	if err != nil || len(kyberPubBytes) != 1184 {
		return nil
	}
	kyberSeedBytes, err := hex.DecodeString(resp.KyberSeed)
	if err != nil || len(kyberSeedBytes) != 64 {
		return nil
	}
	nameBytes, err := hex.DecodeString(resp.NameKey)
	if err != nil {
		return nil
	}

	signingPubEd := decodeAgentField(resp.SigningPublicKeyEd25519, 32)
	signingPrivEd := decodeAgentField(resp.SigningPrivateKeyEd25519, 64)
	signingPubMl := decodeAgentField(resp.SigningPublicKeyMldsa, 1312)
	signingPrivMl := decodeAgentField(resp.SigningPrivateKeyMldsa, 2560)

	var pub, priv [32]byte
	copy(pub[:], pubBytes)
	copy(priv[:], privBytes)
	return &KeyMaterial{
		PublicKey:                pub,
		PrivateKey:               priv,
		KyberPublicKey:           kyberPubBytes,
		KyberSeed:                kyberSeedBytes,
		NameKey:                  nameBytes,
		SigningPublicKeyEd25519:  signingPubEd,
		SigningPrivateKeyEd25519: signingPrivEd,
		SigningPublicKeyMldsa:    signingPubMl,
		SigningPrivateKeyMldsa:   signingPrivMl,
	}
}

func decodeAgentField(hexStr string, expectedLen int) []byte {
	if hexStr == "" {
		return nil
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil || len(b) != expectedLen {
		return nil
	}
	return b
}

func Ping() bool {
	info := ReadAgentFile()
	if info == nil {
		return false
	}
	resp, err := sendRequest(info, "ping")
	return err == nil && resp.OK
}

func Shutdown() error {
	info := ReadAgentFile()
	if info == nil {
		return nil
	}
	_, err := sendRequest(info, "shutdown")
	RemoveAgentFile()
	return err
}

func IsRunning() bool {
	return Ping()
}

func sendRequest(info *AgentInfo, action string) (*Response, error) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(info.Port), 2*time.Second)
	if err != nil {
		RemoveAgentFile()
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(Request{Token: info.Token, Action: action}); err != nil {
		return nil, err
	}

	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
