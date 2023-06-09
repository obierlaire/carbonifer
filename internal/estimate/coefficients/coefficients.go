package coefficients

import (
	"encoding/json"
	"reflect"

	"github.com/carboniferio/carbonifer/internal/data"
	"github.com/carboniferio/carbonifer/internal/providers"
	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
)

type Coefficients struct {
	CPUMinWh       decimal.Decimal `json:"cpu_min_wh"`
	CPUMaxWh       decimal.Decimal `json:"cpu_max_wh"`
	StorageHddWhTb decimal.Decimal `json:"storage_hdd_wh_tb"`
	StorageSsdWhTb decimal.Decimal `json:"storage_ssd_wh_tb"`
	NetworkingWhGb decimal.Decimal `json:"networking_wh_gb"`
	MemoryWhGb     decimal.Decimal `json:"memory_wh_gb"`
	PueAverage     decimal.Decimal `json:"pue_average"`
}

type CoefficientsProviders struct {
	AWS   Coefficients `json:"AWS"`
	GCP   Coefficients `json:"GCP"`
	Azure Coefficients `json:"Azure"`
}

var coefficientsPerProviders *CoefficientsProviders

func GetEnergyCoefficients() *CoefficientsProviders {
	if coefficientsPerProviders == nil {
		energyCoefFile := data.ReadDataFile("energy_coefficients.json")
		err := json.Unmarshal(energyCoefFile, &coefficientsPerProviders)
		if err != nil {
			log.Fatal(err)
		}
	}
	return coefficientsPerProviders
}

func (cps *CoefficientsProviders) GetByProvider(provider providers.Provider) Coefficients {
	return coefficientsPerProviders.GetByProviderName(provider.String())
}

func (cps *CoefficientsProviders) GetByProviderName(name string) Coefficients {
	r := reflect.ValueOf(cps)
	coefficients := reflect.Indirect(r).FieldByName(name)
	return coefficients.Interface().(Coefficients)
}
