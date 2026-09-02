/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
)

type StdioClient struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	mu  sync.Mutex
}

func StartStdio(ctx context.Context, command string) (*StdioClient, error) {
	// Use the native command interpreter so configured stdio servers work on
	// Windows as well as Unix systems. The command remains user configuration;
	// it is never assembled from workspace paths.
	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd.exe", "/C"
	}
	cmd := exec.CommandContext(ctx, shell, flag, command)
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	return &StdioClient{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}
func (c *StdioClient) Call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if e != nil {
		return e
	}
	if _, e = c.in.Write(append(b, '\n')); e != nil {
		return e
	}
	type response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	ch := make(chan struct {
		line []byte
		err  error
	}, 1)
	go func() {
		line, err := c.out.ReadBytes('\n')
		ch <- struct {
			line []byte
			err  error
		}{line, err}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case x := <-ch:
		if x.err != nil {
			return x.err
		}
		var r response
		if e = json.Unmarshal(x.line, &r); e != nil {
			return e
		}
		if r.Error != nil {
			return fmt.Errorf("mcp error: %s", r.Error.Message)
		}
		return json.Unmarshal(r.Result, result)
	}
}

func (c *StdioClient) Notify(ctx context.Context, method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	_, err = c.in.Write(append(b, '\n'))
	return err
}
func (c *StdioClient) Close() error {
	if c == nil {
		return nil
	}
	_ = c.in.Close()
	return c.cmd.Wait()
}
