package main

import (
	"bytes"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type cosignBundle struct {
	MediaType            string                     `json:"mediaType"`
	VerificationMaterial bundleVerificationMaterial `json:"verificationMaterial"`
	MessageSignature     *struct {
		MessageDigest struct {
			Algorithm string `json:"algorithm"`
			Digest    string `json:"digest"`
		} `json:"messageDigest"`
		Signature string `json:"signature"`
	} `json:"messageSignature"`
}

type bundleVerificationMaterial struct {
	Certificate          *bundleCertificate `json:"certificate"`
	X509CertificateChain *struct {
		Certificates []bundleCertificate `json:"certificates"`
	} `json:"x509CertificateChain"`
}

type bundleCertificate struct {
	RawBytes string `json:"rawBytes"`
}

func verifySignatureBundle(path string, descriptor envelopeDescriptor, checksumDigest string) error {
	var bundle cosignBundle
	if err := decodeSingleJSON(path, &bundle); err != nil {
		return fmt.Errorf("decode signature bundle: %w", err)
	}
	if bundle.MediaType != sigstoreBundleMediaType || bundle.MessageSignature == nil {
		return errors.New("signature bundle is incomplete or unsupported")
	}
	if err := verifyBundleCertificate(bundle.VerificationMaterial, descriptor.CertificateIdentity, descriptor.OIDCIssuer); err != nil {
		return fmt.Errorf("signature bundle certificate: %w", err)
	}
	if bundle.MessageSignature.MessageDigest.Algorithm != "SHA2_256" || bundle.MessageSignature.Signature == "" {
		return errors.New("signature bundle does not contain a SHA-256 message signature")
	}
	digest, err := base64.StdEncoding.DecodeString(bundle.MessageSignature.MessageDigest.Digest)
	if err != nil {
		return fmt.Errorf("decode signature subject digest: %w", err)
	}
	expected, err := hex.DecodeString(checksumDigest)
	if err != nil {
		return err
	}
	if !bytes.Equal(digest, expected) {
		return fmt.Errorf("signature bundle subject digest does not match %s", checksumAssetPath)
	}
	return nil
}

type provenanceBundle struct {
	MediaType            string                     `json:"mediaType"`
	VerificationMaterial bundleVerificationMaterial `json:"verificationMaterial"`
	DSSEEnvelope         *struct {
		Payload     string `json:"payload"`
		PayloadType string `json:"payloadType"`
		Signatures  []struct {
			Sig string `json:"sig"`
		} `json:"signatures"`
	} `json:"dsseEnvelope"`
}

type provenanceStatement struct {
	Type          string `json:"_type"`
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Name   string            `json:"name"`
		Digest map[string]string `json:"digest"`
	} `json:"subject"`
	Predicate struct {
		BuildDefinition struct {
			BuildType          string `json:"buildType"`
			ExternalParameters struct {
				Workflow struct {
					Repository string `json:"repository"`
					Path       string `json:"path"`
					Ref        string `json:"ref"`
				} `json:"workflow"`
			} `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI    string            `json:"uri"`
				Digest map[string]string `json:"digest"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func verifyProvenanceBundle(path string, manifest releaseManifest, manifestPath string) error {
	var bundle provenanceBundle
	if err := decodeSingleJSON(path, &bundle); err != nil {
		return fmt.Errorf("decode provenance bundle: %w", err)
	}
	if bundle.MediaType != sigstoreBundleMediaType || bundle.DSSEEnvelope == nil {
		return errors.New("provenance bundle is incomplete or unsupported")
	}
	if err := verifyBundleCertificate(bundle.VerificationMaterial, publisherCertificateIdentity(*manifest.Publisher), githubOIDCIssuer); err != nil {
		return fmt.Errorf("provenance bundle certificate: %w", err)
	}
	if bundle.DSSEEnvelope.PayloadType != "application/vnd.in-toto+json" || len(bundle.DSSEEnvelope.Signatures) == 0 || bundle.DSSEEnvelope.Signatures[0].Sig == "" {
		return errors.New("provenance bundle has no signed in-toto payload")
	}
	payload, err := base64.StdEncoding.DecodeString(bundle.DSSEEnvelope.Payload)
	if err != nil {
		return fmt.Errorf("decode provenance payload: %w", err)
	}
	var statement provenanceStatement
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&statement); err != nil {
		return fmt.Errorf("decode provenance statement: %w", err)
	}
	if statement.Type != inTotoStatementType || statement.PredicateType != slsaProvenanceType || statement.Predicate.BuildDefinition.BuildType != githubWorkflowBuildType {
		return errors.New("provenance statement type is incomplete or unsupported")
	}
	workflow := statement.Predicate.BuildDefinition.ExternalParameters.Workflow
	if workflow.Repository != manifest.Publisher.Repository || strings.TrimPrefix(workflow.Path, "/") != manifest.Publisher.Workflow || workflow.Ref != manifest.Publisher.Ref {
		return errors.New("provenance publisher workflow identity does not match manifest")
	}
	expectedBuilder := publisherCertificateIdentity(*manifest.Publisher)
	if statement.Predicate.RunDetails.Builder.ID != expectedBuilder {
		return fmt.Errorf("provenance builder identity %q does not match %q", statement.Predicate.RunDetails.Builder.ID, expectedBuilder)
	}
	expectedDependencyURI := "git+" + manifest.Publisher.Repository + "@" + manifest.Publisher.Ref
	dependencyMatched := false
	for _, dependency := range statement.Predicate.BuildDefinition.ResolvedDependencies {
		if dependency.URI == expectedDependencyURI && dependency.Digest["gitCommit"] == manifest.SourceCommit {
			dependencyMatched = true
			break
		}
	}
	if !dependencyMatched {
		return errors.New("provenance source ref or commit does not match the release manifest")
	}
	expectedSubjects := make(map[string]string, len(manifest.ProvenanceSubjects))
	for _, name := range manifest.ProvenanceSubjects {
		actualPath := filepath.Join(filepath.Dir(manifestPath), name)
		digest, _, err := hashFile(actualPath)
		if err != nil {
			return err
		}
		expectedSubjects[name] = digest
	}
	if len(statement.Subject) != len(expectedSubjects) {
		return fmt.Errorf("provenance contains %d subjects, want %d", len(statement.Subject), len(expectedSubjects))
	}
	seen := make(map[string]struct{}, len(statement.Subject))
	for _, subject := range statement.Subject {
		if _, duplicate := seen[subject.Name]; duplicate {
			return fmt.Errorf("provenance contains duplicate subject %s", subject.Name)
		}
		seen[subject.Name] = struct{}{}
		expectedDigest, ok := expectedSubjects[subject.Name]
		if !ok {
			return fmt.Errorf("provenance contains undeclared subject %s", subject.Name)
		}
		if len(subject.Digest) != 1 || subject.Digest["sha256"] != expectedDigest {
			return fmt.Errorf("provenance digest mismatch for %s", subject.Name)
		}
	}
	return nil
}

var fulcioOIDCIssuerOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}

func verifyBundleCertificate(material bundleVerificationMaterial, expectedIdentity, expectedIssuer string) error {
	certificates, err := bundleCertificates(material)
	if err != nil {
		return err
	}
	parsed := make([]*x509.Certificate, 0, len(certificates))
	for _, encoded := range certificates {
		raw, err := base64.StdEncoding.DecodeString(encoded.RawBytes)
		if err != nil {
			return fmt.Errorf("decode certificate: %w", err)
		}
		certificate, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("parse certificate: %w", err)
		}
		parsed = append(parsed, certificate)
	}
	certificate := parsed[0]
	if certificate.IsCA {
		return errors.New("first certificate in bundle is not a leaf")
	}
	for _, parent := range parsed[1:] {
		if !parent.IsCA {
			return errors.New("bundle contains more than one leaf certificate")
		}
	}
	if len(certificate.URIs) != 1 || certificate.URIs[0].String() != expectedIdentity {
		return fmt.Errorf("identity does not match %q", expectedIdentity)
	}
	issuer := ""
	for _, extension := range certificate.Extensions {
		if extension.Id.Equal(fulcioOIDCIssuerOID) {
			issuer = string(extension.Value)
			break
		}
	}
	if issuer != expectedIssuer {
		return fmt.Errorf("OIDC issuer does not match %q", expectedIssuer)
	}
	return nil
}

func bundleCertificates(material bundleVerificationMaterial) ([]bundleCertificate, error) {
	hasDirect := material.Certificate != nil
	hasChain := material.X509CertificateChain != nil
	if hasDirect == hasChain {
		return nil, errors.New("bundle must contain exactly one certificate representation")
	}
	if hasDirect {
		if material.Certificate.RawBytes == "" {
			return nil, errors.New("certificate is missing")
		}
		return []bundleCertificate{*material.Certificate}, nil
	}
	if len(material.X509CertificateChain.Certificates) == 0 {
		return nil, errors.New("x509 certificate chain is empty")
	}
	for _, certificate := range material.X509CertificateChain.Certificates {
		if certificate.RawBytes == "" {
			return nil, errors.New("x509 certificate chain contains an empty certificate")
		}
	}
	return material.X509CertificateChain.Certificates, nil
}

func decodeSingleJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(file)
	decodeErr := decoder.Decode(value)
	if decodeErr == nil {
		var extra any
		if trailingErr := decoder.Decode(&extra); !errors.Is(trailingErr, io.EOF) {
			if trailingErr == nil {
				decodeErr = errors.New("unexpected trailing JSON value")
			} else {
				decodeErr = trailingErr
			}
		}
	}
	closeErr := file.Close()
	if decodeErr != nil {
		return decodeErr
	}
	return closeErr
}
