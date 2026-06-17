/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

const (
	secretKindOrgCA          = "org-ca"
	secretKindCABootstrap    = "ca-bootstrap"
	secretKindAdminEnroll    = "admin-enrollment"
	secretKindWorkloadEnroll = "workload-enrollment"
	secretKindAdminMSP       = "admin-msp"
	secretKindAdminTLS       = "admin-tls"

	orgMSPCACertKey = "msp-ca.crt"
	orgMSPCAKeyKey  = "msp-ca.key"
	orgTLSCACertKey = "tls-ca.crt"
	orgTLSCAKeyKey  = "tls-ca.key"

	caBootstrapUsername     = "admin"
	bootstrapPasswordLength = 32
)

type identityAuthority struct {
	mspCACertPEM []byte
	mspCAKey     *ecdsa.PrivateKey
	tlsCACertPEM []byte
	tlsCAKey     *ecdsa.PrivateKey
}

func (r *FabricNetworkReconciler) reconcileIdentityMaterial(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	if err := r.ensureCABootstrapSecret(ctx, net, org, namespace); err != nil {
		return err
	}
	if err := r.ensureAdminEnrollmentSecret(ctx, net, org, namespace); err != nil {
		return err
	}

	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, i))
			if err := r.ensureEnrollmentCredentialSecret(ctx, net, org, namespace, name, componentOrderer, secretKindWorkloadEnroll); err != nil {
				return err
			}
		}
	}

	if org.Peer == nil {
		return nil
	}

	for i := 0; i < org.Peer.Instances; i++ {
		name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i))
		if err := r.ensureEnrollmentCredentialSecret(ctx, net, org, namespace, name, componentPeer, secretKindWorkloadEnroll); err != nil {
			return err
		}
	}

	return nil
}

func (r *FabricNetworkReconciler) reconcileGeneratedIdentityFallback(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	authority, err := r.ensureOrgIdentityAuthority(ctx, net, org, namespace)
	if err != nil {
		return err
	}

	if err := r.ensureAdminIdentitySecrets(ctx, net, org, namespace, authority); err != nil {
		return err
	}

	for _, group := range org.Orderers {
		for i := 0; i < group.Instances; i++ {
			name := sanitizeName(fmt.Sprintf("%s%d", group.Prefix, i))
			if err := r.ensureWorkloadIdentitySecrets(
				ctx,
				net,
				org,
				namespace,
				name,
				componentOrderer,
				workloadDNSNames(name, namespace),
				authority,
			); err != nil {
				return err
			}
		}
	}

	if org.Peer == nil {
		return nil
	}

	for i := 0; i < org.Peer.Instances; i++ {
		name := sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i))
		if err := r.ensureWorkloadIdentitySecrets(
			ctx,
			net,
			org,
			namespace,
			name,
			componentPeer,
			workloadDNSNames(name, namespace),
			authority,
		); err != nil {
			return err
		}
	}

	return nil
}

func orgIdentitySecretName(org fabricopsv1alpha1.Org) string {
	return sanitizeName(org.Organization.Name + "-identity-ca")
}

func caBootstrapSecretName(org fabricopsv1alpha1.Org) string {
	return sanitizeName(org.Organization.Name + "-ca-bootstrap")
}

func adminIdentityName(org fabricopsv1alpha1.Org) string {
	return sanitizeName(org.Organization.Name + "-admin")
}

func adminEnrollmentSecretName(org fabricopsv1alpha1.Org) string {
	return identityEnrollmentSecretName(adminIdentityName(org))
}

func identityEnrollmentSecretName(identityName string) string {
	return sanitizeName(identityName + "-enrollment")
}

func (r *FabricNetworkReconciler) ensureCABootstrapSecret(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	desired, err := buildCABootstrapSecret(net, org, namespace)
	if err != nil {
		return err
	}

	_, err = r.ensureSecret(ctx, desired, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, secretKindCABootstrap, net.Spec.Global.TLS)
	})
	return err
}

func buildCABootstrapSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) (*corev1.Secret, error) {
	password, err := generateBootstrapPassword()
	if err != nil {
		return nil, err
	}

	name := caBootstrapSecretName(org)
	userPass := caBootstrapUsername + ":" + password

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: identityLabels(net, org, componentCA, name, map[string]string{
				labelIdentityKind: secretKindCABootstrap,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			caBootstrapUsernameKey: []byte(caBootstrapUsername),
			caBootstrapPasswordKey: []byte(password),
			caBootstrapUserPassKey: []byte(userPass),
		},
	}, nil
}

func (r *FabricNetworkReconciler) ensureAdminEnrollmentSecret(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) error {
	return r.ensureEnrollmentCredentialSecret(ctx, net, org, namespace, adminIdentityName(org), componentAdmin, secretKindAdminEnroll)
}

func (r *FabricNetworkReconciler) ensureOrgIdentityAuthority(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
) (*identityAuthority, error) {
	desiredAuthority, err := generateIdentityAuthority(org)
	if err != nil {
		return nil, err
	}

	name := orgIdentitySecretName(org)
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: identityLabels(net, org, secretKindOrgCA, name, map[string]string{
				labelIdentityKind: secretKindOrgCA,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			orgMSPCACertKey: desiredAuthority.mspCACertPEM,
			orgMSPCAKeyKey:  pemEncodeECPrivateKey(desiredAuthority.mspCAKey),
			orgTLSCACertKey: desiredAuthority.tlsCACertPEM,
			orgTLSCAKeyKey:  pemEncodeECPrivateKey(desiredAuthority.tlsCAKey),
		},
	}

	secret, err := r.ensureSecret(ctx, desired, orgIdentitySecretValidationError)
	if err != nil {
		return nil, err
	}

	return parseIdentityAuthority(secret)
}

func (r *FabricNetworkReconciler) ensureAdminIdentitySecrets(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	authority *identityAuthority,
) error {
	adminName := adminIdentityName(org)
	mspSecret, err := buildWorkloadMSPSecret(net, org, namespace, adminName, componentAdmin, authority)
	if err != nil {
		return err
	}
	mspSecret.Labels[labelIdentityKind] = secretKindAdminMSP
	if _, err := r.ensureGeneratedSecret(ctx, mspSecret, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, secretKindAdminMSP, net.Spec.Global.TLS)
	}); err != nil {
		return err
	}

	if !net.Spec.Global.TLS {
		return nil
	}

	tlsSecret, err := buildAdminTLSSecret(net, org, namespace, adminName, authority)
	if err != nil {
		return err
	}
	_, err = r.ensureGeneratedSecret(ctx, tlsSecret, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, secretKindAdminTLS, net.Spec.Global.TLS)
	})
	return err
}

func (r *FabricNetworkReconciler) ensureWorkloadIdentitySecrets(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	dnsNames []string,
	authority *identityAuthority,
) error {
	if err := r.ensureEnrollmentCredentialSecret(ctx, net, org, namespace, workloadName, component, secretKindWorkloadEnroll); err != nil {
		return err
	}

	mspSecret, err := buildWorkloadMSPSecret(net, org, namespace, workloadName, component, authority)
	if err != nil {
		return err
	}
	if _, err := r.ensureGeneratedSecret(ctx, mspSecret, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, secretKindMSP, net.Spec.Global.TLS)
	}); err != nil {
		return err
	}

	if !net.Spec.Global.TLS {
		return nil
	}

	tlsSecret, err := buildWorkloadTLSSecret(net, org, namespace, workloadName, component, dnsNames, authority)
	if err != nil {
		return err
	}
	_, err = r.ensureGeneratedSecret(ctx, tlsSecret, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, secretKindTLS, net.Spec.Global.TLS)
	})
	return err
}

func (r *FabricNetworkReconciler) ensureEnrollmentCredentialSecret(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	identityName string,
	component string,
	kind string,
) error {
	desired, err := buildEnrollmentCredentialSecret(net, org, namespace, identityName, component, kind)
	if err != nil {
		return err
	}

	_, err = r.ensureSecret(ctx, desired, func(secret corev1.Secret) string {
		return identitySecretValidationError(secret, kind, net.Spec.Global.TLS)
	})
	return err
}

func buildEnrollmentCredentialSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	identityName string,
	component string,
	kind string,
) (*corev1.Secret, error) {
	password, err := generateBootstrapPassword()
	if err != nil {
		return nil, err
	}

	name := identityEnrollmentSecretName(identityName)
	userPass := identityName + ":" + password

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: identityLabels(net, org, component, identityName, map[string]string{
				labelIdentityKind: kind,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			caBootstrapUsernameKey: []byte(identityName),
			caBootstrapPasswordKey: []byte(password),
			caBootstrapUserPassKey: []byte(userPass),
		},
	}, nil
}

func buildAdminTLSSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	adminName string,
	authority *identityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueWorkloadCertificate(
		adminName,
		componentAdmin,
		nil,
		authority.tlsCACertPEM,
		authority.tlsCAKey,
		x509.KeyUsageDigitalSignature,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	)
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(adminName, secretKindTLS),
			Namespace: namespace,
			Labels: identityLabels(net, org, componentAdmin, adminName, map[string]string{
				labelIdentityKind:   secretKindAdminTLS,
				labelIdentitySource: identitySourceDevGenerated,
				labelWorkload:       adminName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertKey:     authority.tlsCACertPEM,
			tlsClientCertKey: certPEM,
			tlsClientKeyKey:  keyPEM,
		},
	}, nil
}

func buildWorkloadMSPSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	authority *identityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueWorkloadCertificate(
		workloadName,
		component,
		nil,
		authority.mspCACertPEM,
		authority.mspCAKey,
		x509.KeyUsageDigitalSignature,
		nil,
	)
	if err != nil {
		return nil, err
	}

	data := map[string][]byte{
		mspConfigKey:   []byte(mspConfigYAML()),
		mspCACertKey:   authority.mspCACertPEM,
		mspSignCertKey: certPEM,
		mspKeyStoreKey: keyPEM,
	}

	if net.Spec.Global.TLS {
		data[mspTLSCACertKey] = authority.tlsCACertPEM
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(workloadName, secretKindMSP),
			Namespace: namespace,
			Labels: identityLabels(net, org, component, workloadName, map[string]string{
				labelIdentityKind:   secretKindMSP,
				labelIdentitySource: identitySourceDevGenerated,
				labelWorkload:       workloadName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}, nil
}

func buildWorkloadTLSSecret(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	namespace string,
	workloadName string,
	component string,
	dnsNames []string,
	authority *identityAuthority,
) (*corev1.Secret, error) {
	certPEM, keyPEM, err := issueWorkloadCertificate(
		workloadName,
		component,
		dnsNames,
		authority.tlsCACertPEM,
		authority.tlsCAKey,
		x509.KeyUsageDigitalSignature|x509.KeyUsageKeyEncipherment,
		[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	)
	if err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      identitySecretName(workloadName, secretKindTLS),
			Namespace: namespace,
			Labels: identityLabels(net, org, component, workloadName, map[string]string{
				labelIdentityKind:   secretKindTLS,
				labelIdentitySource: identitySourceDevGenerated,
				labelWorkload:       workloadName,
			}),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			tlsCACertKey:     authority.tlsCACertPEM,
			tlsServerCertKey: certPEM,
			tlsServerKeyKey:  keyPEM,
		},
	}, nil
}

func (r *FabricNetworkReconciler) ensureSecret(
	ctx context.Context,
	desired *corev1.Secret,
	validationError func(corev1.Secret) string,
) (corev1.Secret, error) {
	var existing corev1.Secret
	key := client.ObjectKeyFromObject(desired)

	err := r.Get(ctx, key, &existing)
	if apierrors.IsNotFound(err) {
		log := logf.FromContext(ctx)
		log.Info("Creating Secret", "name", desired.Name, "namespace", desired.Namespace)
		return *desired, r.Create(ctx, desired)
	}
	if err != nil {
		return corev1.Secret{}, err
	}

	changed := false
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
		changed = true
	}
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			existing.Labels[key] = value
			changed = true
		}
	}

	if validationError != nil {
		if reason := validationError(existing); reason != "" {
			existing.Data = desired.Data
			existing.Type = desired.Type
			changed = true
		}
	}

	if !changed {
		return existing, nil
	}

	log := logf.FromContext(ctx)
	log.Info("Updating Secret", "name", existing.Name, "namespace", existing.Namespace)
	return existing, r.Update(ctx, &existing)
}

func (r *FabricNetworkReconciler) ensureGeneratedSecret(
	ctx context.Context,
	desired *corev1.Secret,
	validationError func(corev1.Secret) string,
) (corev1.Secret, error) {
	var existing corev1.Secret
	key := client.ObjectKeyFromObject(desired)

	err := r.Get(ctx, key, &existing)
	if apierrors.IsNotFound(err) {
		log := logf.FromContext(ctx)
		log.Info("Creating generated fallback Secret", "name", desired.Name, "namespace", desired.Namespace)
		return *desired, r.Create(ctx, desired)
	}
	if err != nil {
		return corev1.Secret{}, err
	}

	if existing.Labels[labelIdentitySource] == identitySourceFabricCA {
		if validationError == nil || validationError(existing) == "" {
			return existing, nil
		}
	}

	changed := false
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
		changed = true
	}
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			existing.Labels[key] = value
			changed = true
		}
	}

	if validationError != nil {
		if reason := validationError(existing); reason != "" {
			existing.Data = desired.Data
			existing.Type = desired.Type
			changed = true
		}
	}

	if !changed {
		return existing, nil
	}

	log := logf.FromContext(ctx)
	log.Info("Updating generated fallback Secret", "name", existing.Name, "namespace", existing.Namespace)
	return existing, r.Update(ctx, &existing)
}

func identityLabels(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	component string,
	workloadName string,
	extra map[string]string,
) map[string]string {
	labels := orgLabels(net, org, component)
	labels[labelWorkload] = workloadName

	for key, value := range extra {
		labels[key] = value
	}

	return labels
}

func generateIdentityAuthority(org fabricopsv1alpha1.Org) (*identityAuthority, error) {
	mspCert, mspKey, err := generateCertificateAuthority("ca."+org.Organization.Domain, org.Organization.MSPName)
	if err != nil {
		return nil, err
	}

	tlsCert, tlsKey, err := generateCertificateAuthority("tlsca."+org.Organization.Domain, org.Organization.MSPName)
	if err != nil {
		return nil, err
	}

	return &identityAuthority{
		mspCACertPEM: mspCert,
		mspCAKey:     mspKey,
		tlsCACertPEM: tlsCert,
		tlsCAKey:     tlsKey,
	}, nil
}

func generateCertificateAuthority(commonName, organization string) ([]byte, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{organization},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	return pemEncodeCertificate(certDER), key, nil
}

func issueWorkloadCertificate(
	commonName string,
	organizationalUnit string,
	dnsNames []string,
	caCertPEM []byte,
	caKey *ecdsa.PrivateKey,
	keyUsage x509.KeyUsage,
	usages []x509.ExtKeyUsage,
) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	caCert, err := parsePEMCertificate(caCertPEM)
	if err != nil {
		return nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:         commonName,
			OrganizationalUnit: []string{organizationalUnit},
		},
		DNSNames:              dnsNames,
		IPAddresses:           workloadIPAddresses(dnsNames),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	return pemEncodeCertificate(certDER), pemEncodeECPrivateKey(key), nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func generateBootstrapPassword() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, bootstrapPasswordLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func workloadDNSNames(name, namespace string) []string {
	return []string{
		name,
		fmt.Sprintf("%s.%s", name, namespace),
		fmt.Sprintf("%s.%s.svc", name, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", name, namespace),
		"localhost",
	}
}

func workloadIPAddresses(names []string) []net.IP {
	ips := []net.IP{}
	for _, name := range names {
		if ip := net.ParseIP(name); ip != nil {
			ips = append(ips, ip)
		}
	}

	ips = append(ips, net.ParseIP("127.0.0.1"))
	return ips
}

func pemEncodeCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func pemEncodeECPrivateKey(key *ecdsa.PrivateKey) []byte {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil
	}

	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func parseIdentityAuthority(secret corev1.Secret) (*identityAuthority, error) {
	mspCAKey, err := parsePEMECPrivateKey(secret.Data[orgMSPCAKeyKey])
	if err != nil {
		return nil, fmt.Errorf("invalid MSP CA key: %w", err)
	}
	if _, err := parsePEMCertificate(secret.Data[orgMSPCACertKey]); err != nil {
		return nil, fmt.Errorf("invalid MSP CA certificate: %w", err)
	}

	tlsCAKey, err := parsePEMECPrivateKey(secret.Data[orgTLSCAKeyKey])
	if err != nil {
		return nil, fmt.Errorf("invalid TLS CA key: %w", err)
	}
	if _, err := parsePEMCertificate(secret.Data[orgTLSCACertKey]); err != nil {
		return nil, fmt.Errorf("invalid TLS CA certificate: %w", err)
	}

	return &identityAuthority{
		mspCACertPEM: secret.Data[orgMSPCACertKey],
		mspCAKey:     mspCAKey,
		tlsCACertPEM: secret.Data[orgTLSCACertKey],
		tlsCAKey:     tlsCAKey,
	}, nil
}

func orgIdentitySecretValidationError(secret corev1.Secret) string {
	missing := missingSecretKeys(secret, []string{
		orgMSPCACertKey,
		orgMSPCAKeyKey,
		orgTLSCACertKey,
		orgTLSCAKeyKey,
	})
	if len(missing) > 0 {
		return "missing keys: " + strings.Join(missing, ",")
	}

	if _, err := parsePEMCertificate(secret.Data[orgMSPCACertKey]); err != nil {
		return "invalid MSP CA certificate"
	}
	if _, err := parsePEMECPrivateKey(secret.Data[orgMSPCAKeyKey]); err != nil {
		return "invalid MSP CA key"
	}
	if _, err := parsePEMCertificate(secret.Data[orgTLSCACertKey]); err != nil {
		return "invalid TLS CA certificate"
	}
	if _, err := parsePEMECPrivateKey(secret.Data[orgTLSCAKeyKey]); err != nil {
		return "invalid TLS CA key"
	}

	return ""
}

func identitySecretValidationError(secret corev1.Secret, kind string, tlsEnabled bool) string {
	switch kind {
	case secretKindCABootstrap:
		return caBootstrapSecretValidationError(secret)
	case secretKindAdminEnroll:
		return enrollmentCredentialSecretValidationError(secret)
	case secretKindWorkloadEnroll:
		return enrollmentCredentialSecretValidationError(secret)
	case secretKindMSP:
		return mspIdentitySecretValidationError(secret, tlsEnabled)
	case secretKindTLS:
		return tlsIdentitySecretValidationError(secret)
	case secretKindAdminMSP:
		return mspIdentitySecretValidationError(secret, tlsEnabled)
	case secretKindAdminTLS:
		return adminTLSIdentitySecretValidationError(secret)
	default:
		return ""
	}
}

func caBootstrapSecretValidationError(secret corev1.Secret) string {
	return enrollmentCredentialSecretValidationError(secret)
}

func enrollmentCredentialSecretValidationError(secret corev1.Secret) string {
	missing := missingSecretKeys(secret, caBootstrapSecretKeys())
	if len(missing) > 0 {
		return "missing keys: " + strings.Join(missing, ",")
	}

	username := strings.TrimSpace(string(secret.Data[caBootstrapUsernameKey]))
	password := strings.TrimSpace(string(secret.Data[caBootstrapPasswordKey]))
	userPass := strings.TrimSpace(string(secret.Data[caBootstrapUserPassKey]))
	if username == "" {
		return "empty enrollment username"
	}
	if password == "" {
		return "empty enrollment password"
	}
	if userPass != username+":"+password {
		return "enrollment user-pass does not match username/password"
	}

	return ""
}

func mspIdentitySecretValidationError(secret corev1.Secret, tlsEnabled bool) string {
	missing := missingSecretKeys(secret, mspSecretKeys(tlsEnabled))
	if len(missing) > 0 {
		return "missing keys: " + strings.Join(missing, ",")
	}

	if strings.TrimSpace(string(secret.Data[mspConfigKey])) == "" {
		return "empty MSP config"
	}
	if _, err := parsePEMCertificate(secret.Data[mspCACertKey]); err != nil {
		return "invalid MSP CA certificate"
	}
	if tlsEnabled {
		if _, err := parsePEMCertificate(secret.Data[mspTLSCACertKey]); err != nil {
			return "invalid TLS CA certificate"
		}
	}
	signCert, err := parsePEMCertificate(secret.Data[mspSignCertKey])
	if err != nil {
		return "invalid signing certificate"
	}
	if len(signCert.ExtKeyUsage) > 0 {
		return "signing certificate has incompatible extended key usage"
	}
	if _, err := parsePEMPrivateKey(secret.Data[mspKeyStoreKey]); err != nil {
		return "invalid private key"
	}

	return ""
}

func tlsIdentitySecretValidationError(secret corev1.Secret) string {
	missing := missingSecretKeys(secret, tlsSecretKeys())
	if len(missing) > 0 {
		return "missing keys: " + strings.Join(missing, ",")
	}

	if _, err := parsePEMCertificate(secret.Data[tlsCACertKey]); err != nil {
		return "invalid TLS CA certificate"
	}
	if _, err := parsePEMCertificate(secret.Data[tlsServerCertKey]); err != nil {
		return "invalid TLS server certificate"
	}
	if _, err := parsePEMPrivateKey(secret.Data[tlsServerKeyKey]); err != nil {
		return "invalid TLS server key"
	}

	return ""
}

func adminTLSIdentitySecretValidationError(secret corev1.Secret) string {
	missing := missingSecretKeys(secret, []string{
		tlsCACertKey,
		tlsClientCertKey,
		tlsClientKeyKey,
	})
	if len(missing) > 0 {
		return "missing keys: " + strings.Join(missing, ",")
	}

	if _, err := parsePEMCertificate(secret.Data[tlsCACertKey]); err != nil {
		return "invalid TLS CA certificate"
	}
	cert, err := parsePEMCertificate(secret.Data[tlsClientCertKey])
	if err != nil {
		return "invalid TLS client certificate"
	}
	if !hasExtKeyUsage(cert, x509.ExtKeyUsageClientAuth) {
		return "TLS client certificate missing client auth usage"
	}
	if _, err := parsePEMPrivateKey(secret.Data[tlsClientKeyKey]); err != nil {
		return "invalid TLS client key"
	}

	return ""
}

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	for _, actual := range cert.ExtKeyUsage {
		if actual == usage {
			return true
		}
	}

	return false
}

func parsePEMCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("missing certificate PEM block")
	}

	return x509.ParseCertificate(block.Bytes)
}

func parsePEMECPrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	key, err := parsePEMPrivateKey(data)
	if err != nil {
		return nil, err
	}

	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ECDSA")
	}

	return ecKey, nil
}

func parsePEMPrivateKey(data []byte) (any, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("missing private key PEM block")
	}

	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM block %q", block.Type)
	}
}

func mspConfigYAML() string {
	return `NodeOUs:
  Enable: true
  ClientOUIdentifier:
    Certificate: cacerts/ca.pem
    OrganizationalUnitIdentifier: client
  PeerOUIdentifier:
    Certificate: cacerts/ca.pem
    OrganizationalUnitIdentifier: peer
  AdminOUIdentifier:
    Certificate: cacerts/ca.pem
    OrganizationalUnitIdentifier: admin
  OrdererOUIdentifier:
    Certificate: cacerts/ca.pem
    OrganizationalUnitIdentifier: orderer
`
}
