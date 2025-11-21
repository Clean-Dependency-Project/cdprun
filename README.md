# CDPRun

CDPRun is a tool for downloading and managing runtime binaries like Node.js, Python, and more.
It is designed to be a single tool that can be used to download and manage runtime binaries for a variety of languages and frameworks.


## Why CDPRun?

We wanted a tool that will provide us a unified way to download and manage runtime binaries with  security attestation and we wanted a way to download via nexus proxy repository without having to download the binaries from the internet.

## How it works?

CDPRun will download these binaries based on a policy file and publish them as a GitHub Release.

It uses endoflife.date API to get the latest version of the runtime and download the binaries from the internet.

It verifies the integrity of the downloaded binaries using a checksum file , gpg signature and clamav malware scanning.

There is a static website generated which uses the GitHub Release as a source of truth.

## Is it production ready?

It is still Work in Progress 

