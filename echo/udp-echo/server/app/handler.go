/******************************************************
# DESC    : echo package handler
# AUTHOR  : Alex Stocks
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2016-09-04 13:08
# FILE    : handler.go
******************************************************/

package main

import (
	"errors"
	"sync"
	"time"
)

import (
	"fmt"
	"github.com/AlexStocks/getty"
	log "github.com/AlexStocks/log4go"
)

const (
	WritePkgTimeout = 1e8
)

var (
	errTooManySessions = errors.New("Too many echo sessions!")
)

type PackageHandler interface {
	Handle(getty.Session, getty.UDPContext) error
}

////////////////////////////////////////////
// heartbeat handler
////////////////////////////////////////////

type HeartbeatHandler struct{}

func (this *HeartbeatHandler) Handle(session getty.Session, ctx getty.UDPContext) error {
	var (
		pkg *EchoPackage
		ok  bool
	)

	log.Debug("get echo heartbeat udp context{%#v}", ctx)
	if pkg, ok = ctx.Pkg.(*EchoPackage); !ok {
		return fmt.Errorf("illegal @ctx.Pkg:%#v", ctx.Pkg)
	}

	var rspPkg EchoPackage
	rspPkg.H = pkg.H
	rspPkg.B = echoHeartbeatResponseString
	rspPkg.H.Len = uint16(len(rspPkg.B) + 1)

	return session.WritePkg(getty.UDPContext{Pkg: &rspPkg, PeerAddr: ctx.PeerAddr}, WritePkgTimeout)
}

////////////////////////////////////////////
// message handler
////////////////////////////////////////////

type MessageHandler struct{}

func (this *MessageHandler) Handle(session getty.Session, ctx getty.UDPContext) error {
	log.Debug("get echo ctx{%#v}", ctx)
	// write echo message handle logic here.
	return session.WritePkg(ctx, WritePkgTimeout)
}

////////////////////////////////////////////
// EchoMessageHandler
////////////////////////////////////////////

type clientEchoSession struct {
	session getty.Session
	active  time.Time
	reqNum  int32
}

type EchoMessageHandler struct {
	handlers map[uint32]PackageHandler

	rwlock     sync.RWMutex
	sessionMap map[getty.Session]*clientEchoSession
}

func newEchoMessageHandler() *EchoMessageHandler {
	handlers := make(map[uint32]PackageHandler)
	handlers[heartbeatCmd] = &HeartbeatHandler{}
	handlers[echoCmd] = &MessageHandler{}

	return &EchoMessageHandler{sessionMap: make(map[getty.Session]*clientEchoSession), handlers: handlers}
}

func (this *EchoMessageHandler) OnOpen(session getty.Session) error {
	var (
		err error
	)

	this.rwlock.RLock()
	if conf.SessionNumber < len(this.sessionMap) {
		err = errTooManySessions
	}
	this.rwlock.RUnlock()
	if err != nil {
		return err
	}

	log.Info("got session:%s", session.Stat())
	this.rwlock.Lock()
	this.sessionMap[session] = &clientEchoSession{session: session}
	this.rwlock.Unlock()
	return nil
}

func (this *EchoMessageHandler) OnError(session getty.Session, err error) {
	log.Info("session{%s} got error{%v}, will be closed.", session.Stat(), err)
	this.rwlock.Lock()
	delete(this.sessionMap, session)
	this.rwlock.Unlock()
}

func (this *EchoMessageHandler) OnClose(session getty.Session) {
	log.Info("session{%s} is closing......", session.Stat())
	this.rwlock.Lock()
	delete(this.sessionMap, session)
	this.rwlock.Unlock()
}

func (this *EchoMessageHandler) OnMessage(session getty.Session, udpCtx interface{}) {
	ctx, ok := udpCtx.(getty.UDPContext)
	if !ok {
		log.Error("illegal UDPContext{%#v}", udpCtx)
		return
	}

	p, ok := ctx.Pkg.(*EchoPackage)
	if !ok {
		log.Error("illegal pkg{%#v}", ctx.Pkg)
		return
	}

	handler, ok := this.handlers[p.H.Command]
	if !ok {
		log.Error("illegal command{%d}", p.H.Command)
		return
	}
	err := handler.Handle(session, ctx)
	if err != nil {
		this.rwlock.Lock()
		if _, ok := this.sessionMap[session]; ok {
			this.sessionMap[session].active = time.Now()
			this.sessionMap[session].reqNum++
		}
		this.rwlock.Unlock()
	}
}

func (this *EchoMessageHandler) OnCron(session getty.Session) {
	var (
		//flag   bool
		active time.Time
	)
	this.rwlock.RLock()
	if _, ok := this.sessionMap[session]; ok {
		active = session.GetActive()
		if conf.sessionTimeout.Nanoseconds() < time.Since(active).Nanoseconds() {
			//flag = true
			log.Error("session{%s} timeout{%s}, reqNum{%d}",
				session.Stat(), time.Since(active).String(), this.sessionMap[session].reqNum)
		}
	}
	this.rwlock.RUnlock()
	// udp session是根据本地udp socket fd生成的，如果关闭则连同socket也一同关闭了
	//if flag {
	//	this.rwlock.Lock()
	//	delete(this.sessionMap, session)
	//	this.rwlock.Unlock()
	//	session.Close()
	//}
}
