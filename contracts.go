package pluginsdk

import extensionv1 "github.com/7K-Inari/inari-api/contract/extension/v1"

// SupportedBackendContracts returns the contract IDs implemented by this SDK.
// A new slice is returned on every call so callers cannot mutate SDK policy.
func SupportedBackendContracts() []string {
	return []string{extensionv1.BackendContractV1}
}

// ParseBackendDeclaration parses and validates a backend declaration from a
// full extension document or spec object. Missing legacy metadata returns nil.
func ParseBackendDeclaration(data []byte) ([]string, error) {
	contracts, err := extensionv1.DecodeSpec(data)
	if err != nil {
		return nil, err
	}
	if contracts == nil {
		return nil, nil
	}
	if contracts.Backend == nil {
		return nil, extensionv1.NewContractError(extensionv1.CodeContractRequired, "spec.contracts.backend", "declaration is required")
	}
	return append([]string(nil), contracts.Backend...), nil
}
