// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package misc

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// VerifyEip1559Header verifies some header attributes which were changed in EIP-1559,
// - gas limit check
// - basefee check
func VerifyEip1559Header(config *params.ChainConfig, parent, header *types.Header) error {
	// Verify that the gas limit remains within allowed bounds
	parentGasLimit := parent.GasLimit
	if !config.IsLondon(parent.Number) {
		parentGasLimit = parent.GasLimit * config.ElasticityMultiplier()
	}
	if err := VerifyGaslimit(parentGasLimit, header.GasLimit); err != nil {
		return err
	}
	// Verify the header is not malformed
	if header.BaseFee == nil {
		return fmt.Errorf("header is missing baseFee")
	}
	// Verify the baseFee is correct based on the parent header.
	expectedBaseFee := CalcBaseFee(config, parent)
	if header.BaseFee.Cmp(expectedBaseFee) != 0 {
		return fmt.Errorf("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d",
			header.BaseFee, expectedBaseFee, parent.BaseFee, parent.GasUsed)
	}
	return nil
}

// CalcBaseFee calculates the basefee of the header.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header) *big.Int {
	initialBaseFee := new(big.Int).SetUint64(params.InitialBaseFee)
	if !config.IsCalcium(parent.Number) {
		return initialBaseFee
	}

	// If the difficulty is >= CalciumInitialBaseFeeDifficulty (1P), return zero
	if parent.Difficulty.Cmp(params.CalciumInitialBaseFeeDifficulty) >= 0 {
		return initialBaseFee
	}

	// difficulty is < 1P, then increase the base fee base on difficulty hash
	difficulty := new(big.Int).Set(params.CalciumInitialBaseFeeDifficulty)
	difficulty.Sub(difficulty, parent.Difficulty)
	// convert difficulty in hash to 100KH
	difficulty.Div(difficulty, params.Big100Kh)
	baseFee := new(big.Int).Set(params.CalciumBaseFeePer100Kh)
	baseFee.Mul(baseFee, difficulty)
	if baseFee.Cmp(initialBaseFee) < 0 {
		return initialBaseFee
	}

	return baseFee
}
