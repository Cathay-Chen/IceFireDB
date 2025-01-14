/*
 *
 *  * Licensed to the Apache Software Foundation (ASF) under one or more
 *  * contributor license agreements.  See the NOTICE file distributed with
 *  * this work for additional information regarding copyright ownership.
 *  * The ASF licenses this file to You under the Apache License, Version 2.0
 *  * (the "License"); you may not use this file except in compliance with
 *  * the License.  You may obtain a copy of the License at
 *  *
 *  *     http://www.apache.org/licenses/LICENSE-2.0
 *  *
 *  * Unless required by applicable law or agreed to in writing, software
 *  * distributed under the License is distributed on an "AS IS" BASIS,
 *  * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  * See the License for the specific language governing permissions and
 *  * limitations under the License.
 *
 */

package redisCluster

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/IceFireDB/IceFireDB-PubSub/pkg/RedSHandle"
	"github.com/IceFireDB/IceFireDB-PubSub/pkg/rediscluster"
	"github.com/IceFireDB/IceFireDB-PubSub/pkg/router"
)

/**
 * 注册的命令列表
 */
func NewRouter(cluster *rediscluster.Cluster) *Router {
	r := &Router{
		redisCluster: cluster,
		cmd:          make(map[string]router.HandlersChain),
	}
	r.pool.New = func() interface{} {
		return r.allocateContext()
	}
	return r
}

const CMDEXEC = "CMDEXEC"

func (r *Router) InitCMD() {
	r.AddCommand("COMMAND", r.cmdCOMMAND)
	r.AddCommand("PING", r.cmdPING)
	r.AddCommand("QUIT", r.cmdQUIT)
	r.AddCommand(CMDEXEC, r.cmdCMDEXEC)

	r.AddCommand("DEL", r.cmdDEL)
	r.AddCommand("EXISTS", r.cmdEXISTS)
	r.AddCommand("MGET", r.cmdMGET)
}

func (r *Router) Handle(w *RedSHandle.WriterHandle, args []interface{}) error {
	defer func() {
		if r := recover(); r != nil {
			logrus.Error("handle panic", r)
		}
	}()
	cmdType := strings.ToUpper(string(args[0].([]byte)))

	op, ok := router.OpTable[cmdType]

	if !ok || op.Flag.IsNotAllowed() {
		return router.WriteError(w, fmt.Errorf(router.ErrUnknownCommand, cmdType))
	}

	if !op.ArgsVerify(len(args)) {
		return router.WriteError(w, fmt.Errorf(router.ErrArguments, cmdType))
	}

	handlers, ok := r.cmd[cmdType]
	if !ok {
		handlers = r.cmd[CMDEXEC]
	}
	c := r.pool.Get().(*router.Context)
	defer func() {
		c.Reset()
		r.pool.Put(c)
	}()
	c.Index = -1
	c.Writer = w
	c.Args = args
	c.Handlers = handlers
	c.Cmd = cmdType
	c.Op = op.Flag
	c.Reply = nil

	return c.Next()
}

func (r *Router) Sync(args []interface{}) error {
	cmdType := strings.ToUpper(args[0].(string))
	op, ok := router.OpTable[cmdType]
	handlers, ok := r.cmd[cmdType]
	if !ok {
		handlers = r.cmd[CMDEXEC]
	}
	c := r.pool.Get().(*router.Context)
	defer func() {
		c.Reset()
		r.pool.Put(c)
	}()

	c.Index = int8(len(handlers) - 1)
	c.Writer = RedSHandle.NewWriterHandle(io.Discard)
	c.Args = args
	c.Handlers = handlers
	c.Cmd = cmdType
	c.Op = op.Flag
	c.Reply = nil
	return c.Next()
}

var _ router.IRoutes = (*Router)(nil)

type Router struct {
	redisCluster *rediscluster.Cluster
	MiddleWares  router.HandlersChain
	cmd          map[string]router.HandlersChain
	pool         sync.Pool
}

func (r *Router) Use(funcs ...router.HandlerFunc) router.IRoutes {
	r.MiddleWares = append(r.MiddleWares, funcs...)
	return r
}

func (r *Router) AddCommand(operation string, handlers ...router.HandlerFunc) router.IRoutes {
	handlers = r.combineHandlers(handlers)
	r.addRoute(operation, handlers)
	return r
}

func (r *Router) Close() error {
	r.redisCluster.Close()
	return nil
}

func (r *Router) addRoute(operation string, handlers router.HandlersChain) {
	if r.cmd == nil {
		r.cmd = make(map[string]router.HandlersChain)
	}
	r.cmd[operation] = handlers
}

func (r *Router) combineHandlers(handlers router.HandlersChain) router.HandlersChain {
	finalSize := len(r.MiddleWares) + len(handlers)
	if finalSize >= int(router.AbortIndex) {
		panic("too many handlers")
	}
	mergedHandlers := make(router.HandlersChain, finalSize)
	copy(mergedHandlers, r.MiddleWares)
	copy(mergedHandlers[len(r.MiddleWares):], handlers)
	return mergedHandlers
}

func (r *Router) allocateContext() *router.Context {
	return &router.Context{}
}
