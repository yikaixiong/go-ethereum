//版权2022 The Go-Ethereum作者
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

package trie

// Tracer跟踪Trie节点的更改。在Trie行动期间，
//可以从Trie中删除某些节点，而这些删除的节点
//不会被Trie.hasher或Trie.Committer捕获。因此，这些删除了
//根本不会从磁盘中删除节点。示踪剂是一种辅助工具
//用于跟踪Trie的所有插入和删除操作并捕获所有操作
//最终删除节点。
//
//更改的节点可以主要分为两类：叶子
//节点和中间节点。前者被呼叫者插入/删除
//插入/删除后者以遵循Trie规则。
//无论节点嵌入什么，该工具都可以跟踪所有这些工具
//父母与否，但永远不会跟踪ValueNode。
//
//此外，它还用于记录节点的原始值
//当它们从磁盘解析时。节点的预价将
//将来被用来构建反向爆破。
//
//注意示踪剂不是线程安全，呼叫者应负责处理
//并发问题。
type tracer struct {
	insert map[string]struct{}
	delete map[string]struct{}
	origin map[string][]byte
}

// newTracer initializes the tracer for capturing trie changes.
func newTracer() *tracer {
	return &tracer{
		insert: make(map[string]struct{}),
		delete: make(map[string]struct{}),
		origin: make(map[string][]byte),
	}
}

/*
// OnRead跟踪新加载的Trie节点并在内部缓存RLP编码的斑点。
//不要更改功能之外的值，因为它没有深入填充。
func（t *tracer）onRead（key [] byte，val [] byte）{
//示踪剂现在不使用，请稍后删除此检查。
如果t == nil {
返回
}
t.origin [string（key）] = val
}
*/

// Oninsert跟踪新插入的Trie节点。如果已经在删除集中
//（复活的节点），然后将其从删除集中擦除为“未接触”。
func (t *tracer) onInsert(key []byte) {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return
	}
	if _, present := t.delete[string(key)]; present {
		delete(t.delete, string(key))
		return
	}
	t.insert[string(key)] = struct{}{}
}

// OnDelete跟踪新删除的Trie节点。如果已经
//在添加组中，然后将其从添加组中擦除
//因为它没有被触动。
func (t *tracer) onDelete(key []byte) {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return
	}
	if _, present := t.insert[string(key)]; present {
		delete(t.insert, string(key))
		return
	}
	t.delete[string(key)] = struct{}{}
}

// 插入列表以列表格式返回插入的插入的trie节点。
func (t *tracer) insertList() [][]byte {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return nil
	}
	var ret [][]byte
	for key := range t.insert {
		ret = append(ret, []byte(key))
	}
	return ret
}

// deleteList returns the tracked deleted trie nodes in list format.
func (t *tracer) deleteList() [][]byte {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return nil
	}
	var ret [][]byte
	for key := range t.delete {
		ret = append(ret, []byte(key))
	}
	return ret
}

/*
// getPrev返回指定节点的缓存原始值。
func（t *tracer）getPrev（key [] byte）[] byte {
//不要恐慌对非初始化的示踪剂，可以进行测试。
如果t == nil {
返回无
}
返回t.origin [string（key）]
}
*/

//重设清除Tracer跟踪的内容。
func (t *tracer) reset() {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return
	}
	t.insert = make(map[string]struct{})
	t.delete = make(map[string]struct{})
	t.origin = make(map[string][]byte)
}

// copy returns a deep copied tracer instance.
func (t *tracer) copy() *tracer {
	// Tracer isn't used right now, remove this check later.
	if t == nil {
		return nil
	}
	var (
		insert = make(map[string]struct{})
		delete = make(map[string]struct{})
		origin = make(map[string][]byte)
	)
	for key := range t.insert {
		insert[key] = struct{}{}
	}
	for key := range t.delete {
		delete[key] = struct{}{}
	}
	for key, val := range t.origin {
		origin[key] = val
	}
	return &tracer{
		insert: insert,
		delete: delete,
		origin: origin,
	}
}
