package main

func CreditVault(id string, amount float64) {
	Vaults[id] += amount
}

func DebitVault(id string, amount float64) bool {
	if Vaults[id] >= amount {
		Vaults[id] -= amount
		return true
	}
	return false
}

func GetVaultBalance(id string) float64 {
	return Vaults[id]
}

func DistributeSurplusMiddleOut(epochID string, pool float64) {
	// Middle-out algorithm lives here
	// CreditVault(...) for participating IDs
}
