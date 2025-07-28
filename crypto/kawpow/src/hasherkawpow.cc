//#include <node.h>
//#include <node_buffer.h>
//#include <v8.h>
#include "hasherkawpow.h"
#include <stdint.h>
//#include "nan.h"
#include <iostream>
#include "include/ethash.h"
#include "include/ethash.hpp"
#include "include/progpow.hpp"
#include "uint256.h"
#include "helpers.hpp"

//using namespace node;
//using namespace v8;

//#define THROW_ERROR_EXCEPTION(x) Nan::ThrowError(x)

/*
	 NAN_METHOD(hash_one) {

	 if (info.Length() < 3)
	 return THROW_ERROR_EXCEPTION("hasher-kawpow.hash_one - 3 arguments expected.");

	 const ethash::hash256* header_hash_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[0]).ToLocalChecked());
	 uint64_t* nonce64_ptr = (uint64_t*)Buffer::Data(Nan::To<v8::Object>(info[1]).ToLocalChecked());
	 int block_height = info[2]->IntegerValue(Nan::GetCurrentContext()).FromJust();
	 ethash::hash256* mix_out_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[3]).ToLocalChecked());
	 ethash::hash256* hash_out_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[4]).ToLocalChecked());

	 static ethash::epoch_context_ptr context{nullptr, nullptr};

	 const auto epoch_number = ethash::get_epoch_number(block_height);

	 if (!context || context->epoch_number != epoch_number)
	 context = ethash::create_epoch_context(epoch_number);

	 progpow::hash_one(*context, block_height, header_hash_ptr, *nonce64_ptr, mix_out_ptr, hash_out_ptr);
	 }


	 NAN_METHOD(verify) {

	 if (info.Length() < 5)
	 return THROW_ERROR_EXCEPTION("hasher-kawpow.verify - 5 arguments expected.");

	 const ethash::hash256* header_hash_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[0]).ToLocalChecked());
	 uint64_t* nonce64_ptr = (uint64_t*)Buffer::Data(Nan::To<v8::Object>(info[1]).ToLocalChecked());
	 int block_height = info[2]->IntegerValue(Nan::GetCurrentContext()).FromJust();
	 const ethash::hash256* mix_hash_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[3]).ToLocalChecked());
	 ethash::hash256* hash_out_ptr = (ethash::hash256*)Buffer::Data(Nan::To<v8::Object>(info[4]).ToLocalChecked());

	 static ethash::epoch_context_ptr context{nullptr, nullptr};

	 const auto epoch_number = ethash::get_epoch_number(block_height);

	 if (!context || context->epoch_number != epoch_number)
	 context = ethash::create_epoch_context(epoch_number);

	 bool is_valid = progpow::verify(*context, block_height, header_hash_ptr, *mix_hash_ptr, *nonce64_ptr, hash_out_ptr);

	 if (is_valid) {
	 info.GetReturnValue().Set(Nan::True());
	 }
	 else {
	 info.GetReturnValue().Set(Nan::False());
	 }
	 }

	 NAN_MODULE_INIT(init) {
	 Nan::Set(target, Nan::New("hash_one").ToLocalChecked(), Nan::GetFunction(Nan::New<FunctionTemplate>(hash_one)).ToLocalChecked());
	 Nan::Set(target, Nan::New("verify").ToLocalChecked(), Nan::GetFunction(Nan::New<FunctionTemplate>(verify)).ToLocalChecked());
	 }

	 NODE_MODULE(hashermtp, init)
	 */

void kawpow_hash(char *hash, char *mix_hash, const char* header_hash_str, const char* nonce_str, const char* height_str) {
	static ethash::epoch_context_ptr context_light{nullptr, nullptr};
	auto header_hash = to_hash256(header_hash_str);
	//auto mix_hash = to_hash256(mix_hash_str);

	// Convert nonce from string
	uint64_t nNonce;
	errno = 0;
	char *endp = nullptr;
	errno = 0; // strtoull will not set errno if valid
	unsigned long long int n = strtoull(nonce_str, &endp, 16);
	nNonce = (uint64_t) n;

	// Convert height from string
	uint32_t nHeight;
	errno = 0;
	endp = nullptr;
	errno = 0; // strtoul will not set errno if valid
	unsigned long int nH = strtoul(height_str, &endp, 10);
	nHeight = (uint32_t) nH;

	// Check epoch number and context
	int epoch_number = (int) nHeight / ETHASH_EPOCH_LENGTH;
	if (!context_light || context_light->epoch_number != epoch_number) {
		context_light = ethash::create_epoch_context(epoch_number);
		std::cout << "Building new context for epoch: " << epoch_number << std::endl;
	}

	ethash::hash256 mix_out_ptr;
	ethash::hash256 hash_out_ptr;
	progpow::hash_one(*context_light, (int) nHeight, (const ethash::hash256*) &header_hash, nNonce, &mix_out_ptr, &hash_out_ptr);
	strcpy(hash, to_hex(hash_out_ptr).c_str());
	strcpy(mix_hash, to_hex(mix_out_ptr).c_str());
	//printf("digest: %s, hash: %s\n", to_hex(mix_out_ptr).c_str(), to_hex(hash_out_ptr).c_str());
}
