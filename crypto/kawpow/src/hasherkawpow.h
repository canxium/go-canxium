#ifndef KAWPOW_H
#define KAWPOW_H

#ifdef __cplusplus
extern "C" {
#endif

	void kawpow_hash(char *hash, char *mix_hash, const char* header_hash_str, const char* nonce_str, const char* height_str);

#ifdef __cplusplus
}
#endif

#endif
