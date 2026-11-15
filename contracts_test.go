package pluginsdk_test

import (
	"reflect"
	"testing"

	extensionv1 "github.com/7K-Inari/inari-api/contract/extension/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestSupportedBackendContractsIsDefensiveCopy(t *testing.T) {
	got := pluginsdk.SupportedBackendContracts()
	if !reflect.DeepEqual(got, []string{extensionv1.BackendContractV1}) {
		t.Fatalf("contracts = %v", got)
	}
	got[0] = "inari.plugin.v2"
	if next := pluginsdk.SupportedBackendContracts()[0]; next != extensionv1.BackendContractV1 {
		t.Fatalf("caller mutated SDK contract set: %q", next)
	}
}

func TestParseBackendDeclaration(t *testing.T) {
	got, err := pluginsdk.ParseBackendDeclaration([]byte(`spec:
  contracts:
    backend: [inari.plugin.v1, inari.plugin.v2]
    ui: {sdk: "^0.1.8", hostApi: inari.ui.v1}
`))
	if err != nil || !reflect.DeepEqual(got, []string{"inari.plugin.v1", "inari.plugin.v2"}) {
		t.Fatalf("declaration = %v, %v", got, err)
	}
	got, err = pluginsdk.ParseBackendDeclaration([]byte(`{"auth":{"methods":[]}}`))
	if err != nil || got != nil {
		t.Fatalf("legacy declaration = %v, %v; want nil, nil", got, err)
	}
	if _, err := pluginsdk.ParseBackendDeclaration([]byte(`spec:
  contracts:
    ui: {sdk: "^0.1.8", hostApi: inari.ui.v1}
`)); err == nil {
		t.Fatal("UI-only declaration accepted by backend parser")
	}
	if _, err := pluginsdk.ParseBackendDeclaration([]byte(`spec:
  contracts:
    backend: [not-a-contract]
`)); err == nil {
		t.Fatal("malformed backend declaration accepted")
	}
}
