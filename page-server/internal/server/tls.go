package server

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
)

// ConfigureTLS sets up TLS configuration
func ConfigureTLS(server *http.Server, tlsEnabled bool, tlsCertFile, tlsKeyFile string) error {
	if !tlsEnabled {
		return nil // TLS not enabled
	}
	
	if tlsCertFile == "" || tlsKeyFile == "" {
		return fmt.Errorf("TLS enabled but certificate or key file not specified")
	}
	
	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate: %w", err)
	}
	
	// Configure TLS
	server.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12, // Require TLS 1.2 or higher
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
	}
	
	log.Printf("TLS enabled with certificate: %s", tlsCertFile)
	return nil
}

// GenerateSelfSignedCert generates a self-signed certificate for testing
// This is a placeholder - in production, use proper certificates from a CA
func GenerateSelfSignedCert(certFile, keyFile string) error {
	// This would use crypto/x509 to generate a self-signed certificate
	// For now, we'll just log that it's not implemented
	log.Printf("Self-signed certificate generation not implemented")
	log.Printf("Use openssl to generate certificates:")
	log.Printf("  openssl req -x509 -newkey rsa:4096 -keyout %s -out %s -days 365 -nodes", keyFile, certFile)
	return fmt.Errorf("self-signed certificate generation not implemented - use openssl")
}

