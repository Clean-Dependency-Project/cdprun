# RPM Spec file for Python Runtime
# Compiles Python from source and packages it as an RPM for Amazon Linux
#
# Usage: rpmbuild -bb python.spec --define "runtime_version 3.13.11"

%global runtime_version %{?runtime_version}%{!?runtime_version:3.13.11}
%global runtime_release %{?runtime_release}%{!?runtime_release:1}
%global package_name %{?package_name}%{!?package_name:python-%{runtime_version}}
%global source_filename %{?source_filename}%{!?source_filename:Python-%{runtime_version}.tgz}
%global install_prefix %{?install_prefix}%{!?install_prefix:/export/apps/citools/python/python-%{runtime_version}}

%define _install_prefix %{install_prefix}

Name:           %{package_name}
Version:        %{runtime_version}
Release:        %{runtime_release}%{?dist}
Summary:        Python %{runtime_version} runtime

License:        PSF-2.0
URL:            https://www.python.org
Source0:        %{source_filename}

# Disable automatic dependency generation (self-contained installation)
AutoReqProv:    no

# Disable debug package generation
%global debug_package %{nil}

# Disable binary stripping (preserve optimizations)
%define __strip /bin/true

# Disable build-id generation
%define _build_id_links none

# Disable shebang mangling (Python manages its own shebangs)
%undefine __brp_mangle_shebangs

# Disable other build-root policy checks
%define __brp_check_rpaths %{nil}
%define __brp_ldconfig %{nil}

%description
Python %{runtime_version} runtime installed to %{_install_prefix}.
This package provides an isolated Python installation that does not conflict
with system Python. Multiple versions can be installed simultaneously.

Compiled from official python.org source with optimizations enabled.

%prep
%setup -q -n Python-%{runtime_version}

%build
# Configure with optimizations and standard library modules
./configure \
    --prefix=%{_install_prefix} \
    --enable-optimizations \
    --with-lto \
    --with-system-ffi \
    --with-computed-gotos \
    --enable-ipv6 \
    --enable-loadable-sqlite-extensions \
    --with-ensurepip=upgrade

# Build using all available cores
make %{?_smp_mflags}

%install
# Install to the build root
make install DESTDIR=%{buildroot}

%files
%{_install_prefix}

%post
echo "Python %{runtime_version} installed to %{_install_prefix}"
echo "To use: %{_install_prefix}/bin/python3"

%postun
if [ $1 -eq 0 ]; then
    echo "Python %{runtime_version} has been removed from %{_install_prefix}"
fi

%changelog
* Wed Mar 04 2026 CDP Team <cdp@example.com> - 1-1
- Add macro overrides for shared package execute pipeline.

* Tue Feb 04 2025 CDP Team <cdp@example.com> - 1-1
- Initial RPM package for Python (compiled from source)
