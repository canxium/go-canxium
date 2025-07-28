#include "hasherkawpow.h"
#include <cstdio>

int main() {
	const char* header_hash_str = "acd14e518a5e8c8f200e7233e627a63ec32f6f0a47a499a6e4b5f530e153153d";
	const char* mix_hash_str = "8699f74f8edc03ae0c3a908f01da7bbdfa55954e9c1da5b2f17009d455ccdf97";
	const char* nonce_str = "763df482051ed3dd";
	const char* height_str = "274070";

	char hash[65], mix_hash[65];
	for (int i = 0; i < 1000; i++) {
		kawpow_hash(hash, mix_hash, header_hash_str, nonce_str, height_str);
		printf("%d hash: %s, mix: %s\n", i, hash, mix_hash);
	}

	return 0;
}
