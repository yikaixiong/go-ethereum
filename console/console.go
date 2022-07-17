//版权所有2016年作者
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

package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/dop251/goja"
	"github.com/ethereum/go-ethereum/console/prompt"
	"github.com/ethereum/go-ethereum/internal/jsre"
	"github.com/ethereum/go-ethereum/internal/jsre/deps"
	"github.com/ethereum/go-ethereum/internal/web3ext"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/mattn/go-colorable"
	"github.com/peterh/liner"
)

var (
	// u: unlock, s: signXX, sendXX, n: newAccount, i: importXX
	passwordRegexp = regexp.MustCompile(`personal.[nusi]`)
	onlyWhitespace = regexp.MustCompile(`^\s*$`)
	exit           = regexp.MustCompile(`^\s*exit\s*;*\s*$`)
)

// HistoryFile是数据目录中的文件，用于存储输入卷轴。
const HistoryFile = "history"

// DefaultPrompt是用于用户输入查询的默认提示线前缀。
const DefaultPrompt = "> "

// 配置是配置的集合来微调
// JavaScript控制台。
type Config struct {
	DataDir  string              // Data directory to store the console history at
	DocRoot  string              // Filesystem path from where to load JavaScript files from
	Client   *rpc.Client         // RPC client to execute Ethereum requests through
	Prompt   string              // Input prompt prefix string (defaults to DefaultPrompt)
	Prompter prompt.UserPrompter // Input prompter to allow interactive user feedback (defaults to TerminalPrompter)
	Printer  io.Writer           // Output writer to serialize any display strings to (defaults to os.Stdout)
	Preload  []string            // Absolute paths to JavaScript files to preload
}

// 控制台是JavaScript解释的运行时环境。这是一个完全逃跑的
//通过外部或程序中的RPC附加到运行节点上的JavaScript控制台
// 客户。
type Console struct {
	client   *rpc.Client         // RPC client to execute Ethereum requests through
	jsre     *jsre.JSRE          // JavaScript runtime environment running the interpreter
	prompt   string              // Input prompt prefix string
	prompter prompt.UserPrompter // Input prompter to allow interactive user feedback
	histPath string              // Absolute path to the console scrollback history
	history  []string            // Scroll history maintained by the console
	printer  io.Writer           // Output writer to serialize any display strings to

	interactiveStopped chan struct{}
	stopInteractiveCh  chan struct{}
	signalReceived     chan struct{}
	stopped            chan struct{}
	wg                 sync.WaitGroup
	stopOnce           sync.Once
}

// 新初始化JavaScript解释的运行时环境并设置默认值
//与配置结构。
func New(config Config) (*Console, error) {
	// 优雅地处理未设置的配置值
	if config.Prompter == nil {
		config.Prompter = prompt.Stdin
	}
	if config.Prompt == "" {
		config.Prompt = DefaultPrompt
	}
	if config.Printer == nil {
		config.Printer = colorable.NewColorableStdout()
	}

	// Initialize the console and return
	console := &Console{
		client:             config.Client,
		jsre:               jsre.New(config.DocRoot, config.Printer),
		prompt:             config.Prompt,
		prompter:           config.Prompter,
		printer:            config.Printer,
		histPath:           filepath.Join(config.DataDir, HistoryFile),
		interactiveStopped: make(chan struct{}),
		stopInteractiveCh:  make(chan struct{}),
		signalReceived:     make(chan struct{}, 1),
		stopped:            make(chan struct{}),
	}
	if err := os.MkdirAll(config.DataDir, 0700); err != nil {
		return nil, err
	}
	if err := console.init(config.Preload); err != nil {
		return nil, err
	}

	console.wg.Add(1)
	go console.interruptHandler()

	return console, nil
}

// init从远程RPC提供商中检索可用的API并初始化
//基于暴露模块的控制台的JavaScript名称空间。
func (c *Console) init(preload []string) error {
	c.initConsoleObject()

	// 初始化JavaScript <-> GO RPC桥。
	bridge := newBridge(c.client, c.prompter, c.printer)
	if err := c.initWeb3(bridge); err != nil {
		return err
	}
	if err := c.initExtensions(); err != nil {
		return err
	}

	// 为Web3.js功能添加桥梁覆盖。
	c.jsre.Do(func(vm *goja.Runtime) {
		c.initAdmin(vm, bridge)
		c.initPersonal(vm, bridge)
	})

	// 预加载JavaScript文件。
	for _, path := range preload {
		if err := c.jsre.Exec(path); err != nil {
			failure := err.Error()
			if gojaErr, ok := err.(*goja.Exception); ok {
				failure = gojaErr.String()
			}
			return fmt.Errorf("%s: %v", path, failure)
		}
	}

	// 为历史记录和选项卡的完成配置输入提示器。
	if c.prompter != nil {
		if content, err := os.ReadFile(c.histPath); err != nil {
			c.prompter.SetHistory(nil)
		} else {
			c.history = strings.Split(string(content), "\n")
			c.prompter.SetHistory(c.history)
		}
		c.prompter.SetWordCompleter(c.AutoCompleteInput)
	}
	return nil
}

// 初始化console
func (c *Console) initConsoleObject() {
	c.jsre.Do(func(vm *goja.Runtime) {
		console := vm.NewObject()
		console.Set("log", c.consoleOutput)
		console.Set("error", c.consoleOutput)
		vm.Set("console", console)
	})
}

// 初始化web3对象
func (c *Console) initWeb3(bridge *bridge) error {
	if err := c.jsre.Compile("bignumber.js", deps.BigNumberJS); err != nil {
		return fmt.Errorf("bignumber.js: %v", err)
	}
	if err := c.jsre.Compile("web3.js", deps.Web3JS); err != nil {
		return fmt.Errorf("web3.js: %v", err)
	}
	if _, err := c.jsre.Run("var Web3 = require('web3');"); err != nil {
		return fmt.Errorf("web3 require: %v", err)
	}
	var err error
	c.jsre.Do(func(vm *goja.Runtime) {
		transport := vm.NewObject()
		transport.Set("send", jsre.MakeCallback(vm, bridge.Send))
		transport.Set("sendAsync", jsre.MakeCallback(vm, bridge.Send))
		vm.Set("_consoleWeb3Transport", transport)
		_, err = vm.RunString("var web3 = new Web3(_consoleWeb3Transport)")
	})
	return err
}

// initextensions加载和注册Web3.js扩展。
func (c *Console) initExtensions() error {
	// Compute aliases from server-provided modules.
	apis, err := c.client.SupportedModules()
	if err != nil {
		return fmt.Errorf("api modules: %v", err)
	}
	aliases := map[string]struct{}{"eth": {}, "personal": {}}
	for api := range apis {
		if api == "web3" {
			continue
		}
		aliases[api] = struct{}{}
		if file, ok := web3ext.Modules[api]; ok {
			if err = c.jsre.Compile(api+".js", file); err != nil {
				return fmt.Errorf("%s.js: %v", api, err)
			}
		}
	}

	// Apply aliases.
	c.jsre.Do(func(vm *goja.Runtime) {
		web3 := getObject(vm, "web3")
		for name := range aliases {
			if v := web3.Get(name); v != nil {
				vm.Set(name, v)
			}
		}
	})
	return nil
}

// Initadmin创建了桥梁实现的其他管理API。
func (c *Console) initAdmin(vm *goja.Runtime, bridge *bridge) {
	if admin := getObject(vm, "admin"); admin != nil {
		admin.Set("sleepBlocks", jsre.MakeCallback(vm, bridge.SleepBlocks))
		admin.Set("sleep", jsre.MakeCallback(vm, bridge.Sleep))
		admin.Set("clearHistory", c.clearHistory)
	}
}

//通过桥梁的Initpersonal重定向与帐户相关的API方法。
//
//如果控制台处于交互式模式并且“个人” API可用，则覆盖
// openwallet，unlockaccount，newAccount和符号方法，因为这些需要用户
// 相互作用。原始的Web3回调存储在“ Jeth”中。这些将被称为
//提示后通过桥梁，并将原始Web3请求发送到后端。
func (c *Console) initPersonal(vm *goja.Runtime, bridge *bridge) {
	personal := getObject(vm, "personal")
	if personal == nil || c.prompter == nil {
		return
	}
	jeth := vm.NewObject()
	vm.Set("jeth", jeth)
	jeth.Set("openWallet", personal.Get("openWallet"))
	jeth.Set("unlockAccount", personal.Get("unlockAccount"))
	jeth.Set("newAccount", personal.Get("newAccount"))
	jeth.Set("sign", personal.Get("sign"))
	personal.Set("openWallet", jsre.MakeCallback(vm, bridge.OpenWallet))
	personal.Set("unlockAccount", jsre.MakeCallback(vm, bridge.UnlockAccount))
	personal.Set("newAccount", jsre.MakeCallback(vm, bridge.NewAccount))
	personal.Set("sign", jsre.MakeCallback(vm, bridge.Sign))
}

// 清楚历史
func (c *Console) clearHistory() {
	c.history = nil
	c.prompter.ClearHistory()
	if err := os.Remove(c.histPath); err != nil {
		fmt.Fprintln(c.printer, "can't delete history file:", err)
	} else {
		fmt.Fprintln(c.printer, "history file deleted")
	}
}

// consoleOutput is an override for the console.log and console.error methods to
// 将输出传输到配置的输出流而不是Stdout中。
func (c *Console) consoleOutput(call goja.FunctionCall) goja.Value {
	var output []string
	for _, argument := range call.Arguments {
		output = append(output, fmt.Sprintf("%v", argument))
	}
	fmt.Fprintln(c.printer, strings.Join(output, " "))
	return goja.Null()
}

//AutoCompleteInput是用户使用的预组装单词完成器
//输入提示器向用户提供有关可用方法的提示。
func (c *Console) AutoCompleteInput(line string, pos int) (string, []string, string) {
	// No completions can be provided for empty inputs
	if len(line) == 0 || pos == 0 {
		return "", nil, ""
	}
	// Chunck data to relevant part for autocompletion
	// E.g. in case of nested lines eth.getBalance(eth.coinb<tab><tab>
	start := pos - 1
	for ; start > 0; start-- {
		// Skip all methods and namespaces (i.e. including the dot)
		if line[start] == '.' || (line[start] >= 'a' && line[start] <= 'z') || (line[start] >= 'A' && line[start] <= 'Z') {
			continue
		}
		// Handle web3 in a special way (i.e. other numbers aren't auto completed)
		if start >= 3 && line[start-3:start] == "web3" {
			start -= 3
			continue
		}
		// We've hit an unexpected character, autocomplete form here
		start++
		break
	}
	return line[:start], c.jsre.CompleteKeywords(line[start:pos]), line[pos:]
}

// 欢迎节目当前的Geth实例摘要和一些有关
//控制台的可用模块。
func (c *Console) Welcome() {
	message := "Welcome to the Geth JavaScript console!\n\n"

	// Print some generic Geth metadata
	if res, err := c.jsre.Run(`
		var message = "instance: " + web3.version.node + "\n";
		try {
			message += "coinbase: " + eth.coinbase + "\n";
		} catch (err) {}
		message += "at block: " + eth.blockNumber + " (" + new Date(1000 * eth.getBlock(eth.blockNumber).timestamp) + ")\n";
		try {
			message += " datadir: " + admin.datadir + "\n";
		} catch (err) {}
		message
	`); err == nil {
		message += res.String()
	}
	// List all the supported modules for the user to call
	if apis, err := c.client.SupportedModules(); err == nil {
		modules := make([]string, 0, len(apis))
		for api, version := range apis {
			modules = append(modules, fmt.Sprintf("%s:%s", api, version))
		}
		sort.Strings(modules)
		message += " modules: " + strings.Join(modules, " ") + "\n"
	}
	message += "\nTo exit, press ctrl-d or type exit"
	fmt.Fprintln(c.printer, message)
}

// 评估执行代码并将结果打印到指定的输出
// 溪流。
func (c *Console) Evaluate(statement string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(c.printer, "[native] error: %v\n", r)
		}
	}()
	c.jsre.Evaluate(statement, c.printer)

	// Avoid exiting Interactive when jsre was interrupted by SIGINT.
	c.clearSignalReceived()
}

// Interupthandler在自己的goroutine中运行并等待信号。
//当收到信号时，它会中断JS解释器。
func (c *Console) interruptHandler() {
	defer c.wg.Done()

	// During Interactive, liner inhibits the signal while it is prompting for
	// input. However, the signal will be received while evaluating JS.
	//
	// On unsupported terminals, SIGINT can also happen while prompting.
	// Unfortunately, it is not possible to abort the prompt in this case and
	// the c.readLines goroutine leaks.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	defer signal.Stop(sig)

	for {
		select {
		case <-sig:
			c.setSignalReceived()
			c.jsre.Interrupt(errors.New("interrupted"))
		case <-c.stopInteractiveCh:
			close(c.interactiveStopped)
			c.jsre.Interrupt(errors.New("interrupted"))
		case <-c.stopped:
			return
		}
	}
}

func (c *Console) setSignalReceived() {
	select {
	case c.signalReceived <- struct{}{}:
	default:
	}
}

func (c *Console) clearSignalReceived() {
	select {
	case <-c.signalReceived:
	default:
	}
}

// StopInteractive causes Interactive to return as soon as possible.
func (c *Console) StopInteractive() {
	select {
	case c.stopInteractiveCh <- struct{}{}:
	case <-c.stopped:
	}
}

//交互式启动交互式用户会话，其中in。
//配置的用户提示器。
func (c *Console) Interactive() {
	var (
		prompt      = c.prompt             // the current prompt line (used for multi-line inputs)
		indents     = 0                    // the current number of input indents (used for multi-line inputs)
		input       = ""                   // the current user input
		inputLine   = make(chan string, 1) // receives user input
		inputErr    = make(chan error, 1)  // receives liner errors
		requestLine = make(chan string)    // requests a line of input
	)

	defer func() {
		c.writeHistory()
	}()

	// The line reader runs in a separate goroutine.
	go c.readLines(inputLine, inputErr, requestLine)
	defer close(requestLine)

	for {
		// Send the next prompt, triggering an input read.
		requestLine <- prompt

		select {
		case <-c.interactiveStopped:
			fmt.Fprintln(c.printer, "node is down, exiting console")
			return

		case <-c.signalReceived:
			// SIGINT received while prompting for input -> unsupported terminal.
			// I'm not sure if the best choice would be to leave the console running here.
			// Bash keeps running in this case. node.js does not.
			fmt.Fprintln(c.printer, "caught interrupt, exiting")
			return

		case err := <-inputErr:
			if err == liner.ErrPromptAborted {
				// When prompting for multi-line input, the first Ctrl-C resets
				// the multi-line state.
				prompt, indents, input = c.prompt, 0, ""
				continue
			}
			return

		case line := <-inputLine:
			// User input was returned by the prompter, handle special cases.
			if indents <= 0 && exit.MatchString(line) {
				return
			}
			if onlyWhitespace.MatchString(line) {
				continue
			}
			// Append the line to the input and check for multi-line interpretation.
			input += line + "\n"
			indents = countIndents(input)
			if indents <= 0 {
				prompt = c.prompt
			} else {
				prompt = strings.Repeat(".", indents*3) + " "
			}
			// If all the needed lines are present, save the command and run it.
			if indents <= 0 {
				if len(input) > 0 && input[0] != ' ' && !passwordRegexp.MatchString(input) {
					if command := strings.TrimSpace(input); len(c.history) == 0 || command != c.history[len(c.history)-1] {
						c.history = append(c.history, command)
						if c.prompter != nil {
							c.prompter.AppendHistory(command)
						}
					}
				}
				c.Evaluate(input)
				input = ""
			}
		}
	}
}

// 读取线以自己的goroutine运行，提示输入。
func (c *Console) readLines(input chan<- string, errc chan<- error, prompt <-chan string) {
	for p := range prompt {
		line, err := c.prompter.PromptInput(p)
		if err != nil {
			errc <- err
		} else {
			input <- line
		}
	}
}

//Countents返回给定输入的标识数量。
//如果输入无效，例如var a =}结果可能为负。
func countIndents(input string) int {
	var (
		indents     = 0
		inString    = false
		strOpenChar = ' '   // keep track of the string open char to allow var str = "I'm ....";
		charEscaped = false // keep track if the previous char was the '\' char, allow var str = "abc\"def";
	)

	for _, c := range input {
		switch c {
		case '\\':
			// indicate next char as escaped when in string and previous char isn't escaping this backslash
			if !charEscaped && inString {
				charEscaped = true
			}
		case '\'', '"':
			if inString && !charEscaped && strOpenChar == c { // end string
				inString = false
			} else if !inString && !charEscaped { // begin string
				inString = true
				strOpenChar = c
			}
			charEscaped = false
		case '{', '(':
			if !inString { // ignore brackets when in string, allow var str = "a{"; without indenting
				indents++
			}
			charEscaped = false
		case '}', ')':
			if !inString {
				indents--
			}
			charEscaped = false
		default:
			charEscaped = false
		}
	}

	return indents
}

// 停止清理控制台并终止运行时环境。
func (c *Console) Stop(graceful bool) error {
	c.stopOnce.Do(func() {
		// Stop the interrupt handler.
		close(c.stopped)
		c.wg.Wait()
	})

	c.jsre.Stop(graceful)
	return nil
}

func (c *Console) writeHistory() error {
	if err := os.WriteFile(c.histPath, []byte(strings.Join(c.history, "\n")), 0600); err != nil {
		return err
	}
	return os.Chmod(c.histPath, 0600) // Force 0600, even if it was different previously
}
