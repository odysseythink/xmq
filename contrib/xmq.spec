%define name xmq
%define version 1.1.1-alpha
%define release 1
%define path usr/local
%define group Database/Applications
%define __os_install_post %{nil}

Summary:    xmq
Name:       %{name}
Version:    %{version}
Release:    %{release}
Group:      %{group}
Packager:   Matt Reiferson <mreiferson@gmail.com>
License:    Apache
BuildRoot:  %{_tmppath}/%{name}-%{version}-%{release}
AutoReqProv: no
# we just assume you have go installed. You may or may not have an RPM to depend on.
# BuildRequires: go

%description 
XMQ - A realtime distributed messaging platform
https://mlib.com/xmq

%prep
mkdir -p $RPM_BUILD_DIR/%{name}-%{version}-%{release}
cd $RPM_BUILD_DIR/%{name}-%{version}-%{release}
git clone git@github.com:xmqio/xmq.git

%build
cd $RPM_BUILD_DIR/%{name}-%{version}-%{release}/xmq
make PREFIX=/%{path}

%install
export DONT_STRIP=1
rm -rf $RPM_BUILD_ROOT
cd $RPM_BUILD_DIR/%{name}-%{version}-%{release}/xmq
make PREFIX=/${path} DESTDIR=$RPM_BUILD_ROOT install

%files
/%{path}/bin/xmqadmin
/%{path}/bin/xmqd
/%{path}/bin/xmqlookupd
/%{path}/bin/xmq_to_file
/%{path}/bin/xmq_to_http
/%{path}/bin/xmq_to_xmq
/%{path}/bin/xmq_tail
/%{path}/bin/xmq_stat
/%{path}/bin/to_xmq
