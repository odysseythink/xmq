PREFIX=/usr/local
BINDIR=${PREFIX}/bin
DESTDIR=
BLDDIR = build
BLDFLAGS=
EXT=
ifeq (${GOOS},windows)
    EXT=.exe
endif

APPS = xmqd xmqlookupd xmqadmin xmq_to_xmq xmq_to_file xmq_to_http xmq_tail xmq_stat to_xmq
all: $(APPS)

$(BLDDIR)/xmqd:        $(wildcard apps/xmqd/*.go       xmqd/*.go       xmq/*.go internal/*/*.go)
$(BLDDIR)/xmqlookupd:  $(wildcard apps/xmqlookupd/*.go xmqlookupd/*.go xmq/*.go internal/*/*.go)
$(BLDDIR)/xmqadmin:    $(wildcard apps/xmqadmin/*.go   xmqadmin/*.go xmqadmin/templates/*.go internal/*/*.go)
$(BLDDIR)/xmq_to_xmq:  $(wildcard apps/xmq_to_xmq/*.go  xmq/*.go internal/*/*.go)
$(BLDDIR)/xmq_to_file: $(wildcard apps/xmq_to_file/*.go xmq/*.go internal/*/*.go)
$(BLDDIR)/xmq_to_http: $(wildcard apps/xmq_to_http/*.go xmq/*.go internal/*/*.go)
$(BLDDIR)/xmq_tail:    $(wildcard apps/xmq_tail/*.go    xmq/*.go internal/*/*.go)
$(BLDDIR)/xmq_stat:    $(wildcard apps/xmq_stat/*.go             internal/*/*.go)
$(BLDDIR)/to_xmq:      $(wildcard apps/to_xmq/*.go               internal/*/*.go)

$(BLDDIR)/%:
	@mkdir -p $(dir $@)
	go build ${BLDFLAGS} -o $@ ./apps/$*

$(APPS): %: $(BLDDIR)/%

clean:
	rm -fr $(BLDDIR)

.PHONY: install clean all
.PHONY: $(APPS)

install: $(APPS)
	install -m 755 -d ${DESTDIR}${BINDIR}
	for APP in $^ ; do install -m 755 ${BLDDIR}/$$APP ${DESTDIR}${BINDIR}/$$APP${EXT} ; done
