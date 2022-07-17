//版权所有2015年作者
//此文件是Go-Ethereum库的一部分。
//
// Go-Ethereum库是免费软件：您可以重新分发它和/或修改
//根据GNU较少的通用公共许可条款的条款，
//免费软件基金会（许可证的3版本）或
//（根据您的选择）任何以后的版本。
//
// go-ethereum库是为了希望它有用，
//但没有任何保修；甚至没有暗示的保证
//适合或适合特定目的的健身。看到
// GNU较少的通用公共许可证以获取更多详细信息。
//
//您应该收到GNU较少的通用公共许可证的副本
//与Go-Ethereum库一起。如果不是，请参见<http://www.gnu.org/licenses/>。

//包装编译器包装ABI编译输出。
package compiler

import (
	"encoding/json"
	"fmt"
)

//  - 合并输出格式
type solcOutput struct {
	Contracts map[string]struct {
		BinRuntime                                  string `json:"bin-runtime"`
		SrcMapRuntime                               string `json:"srcmap-runtime"`
		Bin, SrcMap, Abi, Devdoc, Userdoc, Metadata string
		Hashes                                      map[string]string
	}
	Version string
}

// 坚固v.0.8改变了ABI，DEVDOC和USERDOC的方式
type solcOutputV8 struct {
	Contracts map[string]struct {
		BinRuntime            string `json:"bin-runtime"`
		SrcMapRuntime         string `json:"srcmap-runtime"`
		Bin, SrcMap, Metadata string
		Abi                   interface{}
		Devdoc                interface{}
		Userdoc               interface{}
		Hashes                map[string]string
	}
	Version string
}

// parsecombinedjson进行SOLC的直接输出 - 合并输出运行和
//将其解析为字符串合同名称的地图，以将其分配给合同结构。这
//提供的来源，语言和编译器版本以及编译器选项都是
//通过合同结构。
//
//预计SOLC输出将包含ABI，源映射，用户文档和开发文档。
//
//如果JSON畸形或缺少数据，或者JSON返回错误
//嵌入JSON中的嵌入。
func ParseCombinedJSON(combinedJSON []byte, source string, languageVersion string, compilerVersion string, compilerOptions string) (map[string]*Contract, error) {
	var output solcOutput
	if err := json.Unmarshal(combinedJSON, &output); err != nil {
		// Try to parse the output with the new solidity v.0.8.0 rules
		return parseCombinedJSONV8(combinedJSON, source, languageVersion, compilerVersion, compilerOptions)
	}
	// Compilation succeeded, assemble and return the contracts.
	contracts := make(map[string]*Contract)
	for name, info := range output.Contracts {
		// Parse the individual compilation results.
		var abi interface{}
		if err := json.Unmarshal([]byte(info.Abi), &abi); err != nil {
			return nil, fmt.Errorf("solc: error reading abi definition (%v)", err)
		}
		var userdoc, devdoc interface{}
		json.Unmarshal([]byte(info.Userdoc), &userdoc)
		json.Unmarshal([]byte(info.Devdoc), &devdoc)

		contracts[name] = &Contract{
			Code:        "0x" + info.Bin,
			RuntimeCode: "0x" + info.BinRuntime,
			Hashes:      info.Hashes,
			Info: ContractInfo{
				Source:          source,
				Language:        "Solidity",
				LanguageVersion: languageVersion,
				CompilerVersion: compilerVersion,
				CompilerOptions: compilerOptions,
				SrcMap:          info.SrcMap,
				SrcMapRuntime:   info.SrcMapRuntime,
				AbiDefinition:   abi,
				UserDoc:         userdoc,
				DeveloperDoc:    devdoc,
				Metadata:        info.Metadata,
			},
		}
	}
	return contracts, nil
}

// parsecombinedjsonv8解析Solc的直接输出 - 合并输出
//并使用Solidity v.0.8.0及以后的规则对其进行解析。
func parseCombinedJSONV8(combinedJSON []byte, source string, languageVersion string, compilerVersion string, compilerOptions string) (map[string]*Contract, error) {
	var output solcOutputV8
	if err := json.Unmarshal(combinedJSON, &output); err != nil {
		return nil, err
	}
	// Compilation succeeded, assemble and return the contracts.
	contracts := make(map[string]*Contract)
	for name, info := range output.Contracts {
		contracts[name] = &Contract{
			Code:        "0x" + info.Bin,
			RuntimeCode: "0x" + info.BinRuntime,
			Hashes:      info.Hashes,
			Info: ContractInfo{
				Source:          source,
				Language:        "Solidity",
				LanguageVersion: languageVersion,
				CompilerVersion: compilerVersion,
				CompilerOptions: compilerOptions,
				SrcMap:          info.SrcMap,
				SrcMapRuntime:   info.SrcMapRuntime,
				AbiDefinition:   info.Abi,
				UserDoc:         info.Userdoc,
				DeveloperDoc:    info.Devdoc,
				Metadata:        info.Metadata,
			},
		}
	}
	return contracts, nil
}
