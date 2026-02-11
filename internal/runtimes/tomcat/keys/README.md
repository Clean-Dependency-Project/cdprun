# Tomcat GPG Keys

This directory contains PGP public keys for verifying Apache Tomcat downloads.

## Key Sources
- **Official KEYS files**: Apache Tomcat KEYS files from tomcat-8-KEYS, tomcat-9-KEYS, tomcat-10-KEYS, tomcat-11-KEYS
- **Extraction date**: 2025-01-23
- **Total keys**: 17 unique keys (duplicates across versions were automatically detected and skipped)

## Key Extraction Process

Keys were extracted from the original KEYS files using a Python script that:
1. Parsed PGP key blocks using regex
2. Used GPG to extract fingerprints for each key
3. Saved each key with its fingerprint as the filename
4. Automatically detected and skipped duplicate keys across different KEYS files

## Key List

The following keys are included for verifying Tomcat downloads across all supported versions:

| Filename | Note |
|----------|------|
| 05AB33110949707C93A279E3D3EFE6B686867BA6.asc | Extracted from KEYS files |
| 07E48665A34DCAFAE522E5E6266191C37C037D42.asc | Extracted from KEYS files |
| 47309207D818FFD8DCD3F83F1931D684307A10A5.asc | Extracted from KEYS files |
| 48F8E69F6390C9F25CFEDCD268248959359E722B.asc | Extracted from KEYS files |
| 541FBE7D8F78B25E055DDEE13C370389288584E7.asc | Extracted from KEYS files |
| 5C3C5F3E314C866292F359A8F3AD5C94A67F707E.asc | Extracted from KEYS files |
| 61B832AC2F1C5A90F0F9B00A1C506407564C17A3.asc | Extracted from KEYS files |
| 765908099ACF92702C7D949BFA0C35EA8AA299F1.asc | Extracted from KEYS files |
| 79F7026C690BAA50B92CD8B66A3AD3F4F22C4FED.asc | Extracted from KEYS files |
| 8B46CA49EF4837B8C7F292DAA54AD08EA7A0233C.asc | Extracted from KEYS files |
| 9BA44C2621385CB966EBA586F72C284D731FABEE.asc | Extracted from KEYS files |
| A27677289986DB50844682F8ACB77FC2E86E29AC.asc | Extracted from KEYS files |
| A9C5DF4D22E99998D9875A5110C01C5A2F6059E7.asc | Mark E D Thomas - Key maintainer |
| DCFD35E0BF8CA7344752DE8B6FB21E8933C60243.asc | Extracted from KEYS files |
| F3A04C595DB5B6A5F1ECA43E3B7BBB100D811BBE.asc | Extracted from KEYS files |
| F7DA48BB64BCB84ECBA7EE6935CD23C10D498E23.asc | Extracted from KEYS files |
| markt.asc | Mark Thomas - Pre-existing individual key file |

## Verification

Keys are automatically loaded using the standard GPG pattern:
- Embedded with `//go:embed keys/*.asc`
- Loaded with `gpg.LoadKeyRingFromEmbedFS(embeddedTomcatKeys, "keys")`
- Verified with `gpg.VerifyDetachedSignature(keyRing, dataFile, signatureFile)`

## Migration from Custom Pattern

This directory represents the migration from Tomcat's previous custom PGP key handling to the standard pattern used by all other runtimes (Node.js, Temurin, Python).

**Before**: Custom `extractPGPKeys()` function parsing KEYS files at runtime
**After**: Standard `gpg.LoadKeyRingFromEmbedFS()` loading individual .asc files

This ensures consistency, maintainability, and reliability across all runtime implementations. 