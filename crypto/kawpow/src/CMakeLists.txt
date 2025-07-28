CMAKE_MINIMUM_REQUIRED(VERSION 3.5.1)

project(kawpow)

set(CMAKE_CXX_STANDARD 11)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_C_FLAGS "${CMAKE_C_FLAGS} -pthread")
set(CMAKE_POSITION_INDEPENDENT_CODE ON)

SET(hasher_kawpow_SRC
        "ethash/ethash.cpp"
        "ethash/primes.c"
        "ethash/progpow.cpp"
        "keccak/keccak.c"
        "keccak/keccakf800.c"
        "keccak/keccakf1600.c"
				"uint256.cpp"
				"utilstrencodings.cpp"
				"hasherkawpow.cc"
        )

INCLUDE_DIRECTORIES(.)

ADD_LIBRARY(kawpow STATIC ${hasher_kawpow_SRC})
SET_TARGET_PROPERTIES(kawpow PROPERTIES VERSION 0.1)
SET(LIBRARY_OUTPUT_PATH ${PROJECT_BINARY_DIR}/lib)
INSTALL(TARGETS kawpow
	LIBRARY DESTINATION lib
	ARCHIVE DESTINATION lib)

