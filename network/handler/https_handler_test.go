package handler

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DesKaOne/deskaone-sdk/proxy"
)

func testHTTPSProxy(t *testing.T, handler http.Handler) (*httptest.Server, *proxy.ProxyConfig) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	u, err := url.Parse(srv.URL)
	if err != nil { t.Fatal(err) }
	host, portText, err := net.SplitHostPort(u.Host)
	if err != nil { t.Fatal(err) }
	port, err := strconv.Atoi(portText)
	if err != nil { t.Fatal(err) }
	return srv, &proxy.ProxyConfig{Type: proxy.ProxyTypeHTTPS, Host:host, Port:port}
}

func TestHttpsHandlerConnTunnel(t *testing.T) {
	username, password := "tester", "secret"
	requests := make(chan error, 1)
	srv, cfg := testHTTPSProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
		if r.Method != http.MethodConnect || r.Host != "example.com:443" || r.Header.Get("Proxy-Authorization") != want {
			requests <- fmt.Errorf("unexpected CONNECT request: method=%q host=%q auth=%q",r.Method,r.Host,r.Header.Get("Proxy-Authorization"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- nil
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil { return }
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\nhello")
	}))
	defer srv.Close()
	cfg.Username, cfg.Password = &username, &password

	conn, err := net.DialTimeout("tcp", cfg.Address(), 2*time.Second)
	if err != nil { t.Fatal(err) }
	defer conn.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	tunnel, err := httpsHandlerConnWithTLSConfig(conn,cfg,"example.com",443,2*time.Second,&tls.Config{RootCAs:roots})
	if err != nil { t.Fatal(err) }
	if err := <-requests; err != nil { t.Fatal(err) }
	got := make([]byte,5)
	if _, err := io.ReadFull(tunnel, got); err != nil { t.Fatal(err) }
	if string(got)!="hello" { t.Fatalf("unexpected tunneled data %q", got) }
}

func TestHttpsHandlerConnUntrustedCertificate(t *testing.T) {
	srv, cfg := testHTTPSProxy(t, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){}))
	defer srv.Close()
	conn, err := net.DialTimeout("tcp",cfg.Address(),2*time.Second)
	if err != nil {t.Fatal(err)}
	defer conn.Close()
	_, err = HttpsHandlerConn(conn,cfg,"example.com",443,2*time.Second)
	if err == nil || !strings.Contains(err.Error(),"TLS handshake") {t.Fatalf("expected TLS verification error, got %v",err)}
}

func TestHttpsHandlerConnProxyAuthRequired(t *testing.T) {
	srv, cfg := testHTTPSProxy(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
		w.Header().Set("Proxy-Authenticate",`Basic realm="proxy"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer srv.Close()
	conn, err := net.DialTimeout("tcp",cfg.Address(),2*time.Second)
	if err != nil {t.Fatal(err)}
	defer conn.Close()
	roots:=x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	_, err=httpsHandlerConnWithTLSConfig(conn,cfg,"example.com",443,2*time.Second,&tls.Config{RootCAs:roots})
	if err == nil || !strings.Contains(err.Error(),"status=407") {t.Fatalf("expected status 407, got %v",err)}
}
