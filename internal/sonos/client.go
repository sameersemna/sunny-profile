package sonos

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// SonosOne is a client for a Sonos One speaker
type SonosOne struct {
	IP   string
	Port int
	name string
	http *http.Client
}

func NewSonosOne(ip string) *SonosOne {
	return &SonosOne{IP: ip, Port: 1400, http: &http.Client{Timeout: 10 * time.Second}}
}

func (s *SonosOne) Name() string {
	if s.name != "" {
		return s.name
	}
	return "Sonos@" + s.IP
}

// Discover finds Sonos speakers on the LAN via UPnP SSDP
func Discover(ctx context.Context) ([]*SonosOne, error) {
	addr, err := net.ResolveUDPAddr("udp", "239.255.255.250:1900")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	msg := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 3\r\n" +
		"ST: urn:schemas-upnp-org:device:ZonePlayer:1\r\n" +
		"\r\n"
	conn.WriteTo([]byte(msg), addr)
	conn.SetDeadline(time.Now().Add(4 * time.Second))

	seen := map[string]bool{}
	var speakers []*SonosOne
	buf := make([]byte, 2048)
	for {
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		resp := string(buf[:n])
		ip := src.(*net.UDPAddr).IP.String()
		if !seen[ip] && strings.Contains(resp, "Sonos") {
			seen[ip] = true
			sp := NewSonosOne(ip)
			_ = sp.fetchInfo()
			speakers = append(speakers, sp)
		}
	}
	return speakers, nil
}

func (s *SonosOne) fetchInfo() error {
	resp, err := s.http.Get(fmt.Sprintf("http://%s:%d/xml/device_description.xml", s.IP, s.Port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var desc struct {
		Device struct {
			FriendlyName string `xml:"friendlyName"`
		} `xml:"device"`
	}
	xml.Unmarshal(body, &desc)
	s.name = desc.Device.FriendlyName
	return nil
}

func (s *SonosOne) soap(service, action, body string) (string, error) {
	envelope := fmt.Sprintf(
		`<?xml version="1.0"?>`+
			`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" `+
			`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`+
			`<s:Body>%s</s:Body></s:Envelope>`, body)

	u := fmt.Sprintf("http://%s:%d%s", s.IP, s.Port, service)
	req, err := http.NewRequest("POST", u, strings.NewReader(envelope))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	urns := map[string]string{
		"/MediaRenderer/AVTransport/Control":      "urn:schemas-upnp-org:service:AVTransport:1",
		"/MediaRenderer/RenderingControl/Control": "urn:schemas-upnp-org:service:RenderingControl:1",
	}
	urn := urns[service]
	req.Header.Set("SOAPAction", fmt.Sprintf(`"%s#%s"`, urn, action))

	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

func xmlValue(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	start := i + len(open)
	end := strings.Index(s[start:], close)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

// SetVolume sets the Sonos volume 0-100
func (s *SonosOne) SetVolume(vol int) error {
	body := fmt.Sprintf(
		`<u:SetVolume xmlns:u="urn:schemas-upnp-org:service:RenderingControl:1">`+
			`<InstanceID>0</InstanceID><Channel>Master</Channel><DesiredVolume>%d</DesiredVolume></u:SetVolume>`, vol)
	_, err := s.soap("/MediaRenderer/RenderingControl/Control", "SetVolume", body)
	return err
}

// GetVolume returns the current volume
func (s *SonosOne) GetVolume() (int, error) {
	body := `<u:GetVolume xmlns:u="urn:schemas-upnp-org:service:RenderingControl:1">` +
		`<InstanceID>0</InstanceID><Channel>Master</Channel></u:GetVolume>`
	resp, err := s.soap("/MediaRenderer/RenderingControl/Control", "GetVolume", body)
	if err != nil {
		return 0, err
	}
	var vol int
	fmt.Sscanf(xmlValue(resp, "CurrentVolume"), "%d", &vol)
	return vol, nil
}

// PlayAudioURL plays a remote audio file on the Sonos
func (s *SonosOne) PlayAudioURL(audioURL string) error {
	setBody := fmt.Sprintf(
		`<u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">`+
			`<InstanceID>0</InstanceID><CurrentURI>%s</CurrentURI><CurrentURIMetaData></CurrentURIMetaData></u:SetAVTransportURI>`,
		audioURL)
	if _, err := s.soap("/MediaRenderer/AVTransport/Control", "SetAVTransportURI", setBody); err != nil {
		return err
	}
	playBody := `<u:Play xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">` +
		`<InstanceID>0</InstanceID><Speed>1</Speed></u:Play>`
	_, err := s.soap("/MediaRenderer/AVTransport/Control", "Play", playBody)
	return err
}

// Stop stops playback
func (s *SonosOne) Stop() error {
	body := `<u:Stop xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><InstanceID>0</InstanceID></u:Stop>`
	_, err := s.soap("/MediaRenderer/AVTransport/Control", "Stop", body)
	return err
}

// SayTTS requests TTS audio from localTTSServerURL and plays it through the Sonos
func (s *SonosOne) SayTTS(ctx context.Context, text, ttsServerURL string) error {
	payload, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, "POST", ttsServerURL+"/tts/generate", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("TTS generate: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	return s.PlayAudioURL(result.URL)
}
