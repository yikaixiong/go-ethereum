//版权所有2019年作者
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

package graphql

import (
	"encoding/json"
	"net/http"

	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/node"
	"github.com/graph-gophers/graphql-go"
)

type handler struct {
	Schema *graphql.Schema
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Query         string                 `json:"query"`
		OperationName string                 `json:"operationName"`
		Variables     map[string]interface{} `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := h.Schema.Exec(r.Context(), params.Query, params.OperationName, params.Variables)
	responseJSON, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(response.Errors) > 0 {
		w.WriteHeader(http.StatusBadRequest)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(responseJSON)

}

// New constructs a new GraphQL service instance.
func New(stack *node.Node, backend ethapi.Backend, cors, vhosts []string) error {
	if backend == nil {
		panic("missing backend")
	}
	// check if http server with given endpoint exists and enable graphQL on it
	return newHandler(stack, backend, cors, vhosts)
}

//Newhandler返回一个新的http.handler`将回答GraphQl查询。
//它还在 /端点上导出交互式查询浏览器。
func newHandler(stack *node.Node, backend ethapi.Backend, cors, vhosts []string) error {
	q := Resolver{backend}

	s, err := graphql.ParseSchema(schema, &q)
	if err != nil {
		return err
	}
	h := handler{Schema: s}
	handler := node.NewHTTPHandlerStack(h, cors, vhosts, nil)

	stack.RegisterHandler("GraphQL UI", "/graphql/ui", GraphiQL{})
	stack.RegisterHandler("GraphQL", "/graphql", handler)
	stack.RegisterHandler("GraphQL", "/graphql/", handler)

	return nil
}
