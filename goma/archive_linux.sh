#!/bin/bash
# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script produces the following in the output location
# of the ninja rule:
# 1. lib/goma_inputprocessor.a: an archive with all .o files
#    relevant to goma input processing.
# 2. lib/libc++.a: an archive with libc++ compiled within the
#    goma build.
# 3. lib/libc++abi.a: same as above but for libc++abi.
# 4. include/: All headers generated during the goma build.

shopt -s globstar
# OUTDIR is the output location of the goma build with ninja.
OUTDIR=$1

# INSTALLDIR is the destination directory where archives and
# generated headers should be written.
INSTALLDIR=$2

# If Goma is built for scandeps service, some objects are duplicated or otherwise unneccessary.
GOMA_SERVICE=$3

echo rcsD $INSTALLDIR/lib/goma_input_processor.a > $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/client/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/glog/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/lib/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/base/base/*.o >> $INSTALLDIR/lib/lib.rsp

if [ "x$GOMA_SERVICE" != "xtrue" ]; then
    ls $OUTDIR/obj/third_party/abseil/abseil/*.o >> $INSTALLDIR/lib/lib.rsp
else
    ls $OUTDIR/obj/third_party/abseil/abseil/*.o | grep -v "symbolize.o" >> $INSTALLDIR/lib/lib.rsp
fi

ls $OUTDIR/obj/third_party/abseil/abseil_internal/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/chromium_base/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/protobuf/protobuf_full/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/jsoncpp/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/boringssl/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/zlib/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/zlib_x86_simd/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/zlib_adler32_simd/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/zlib_inflate_chunk_simd/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/zlib_crc32_simd/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/breakpad/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/libyaml/**/*.o >> $INSTALLDIR/lib/lib.rsp
ls $OUTDIR/obj/third_party/minizip/**/*.o >> $INSTALLDIR/lib/lib.rsp
ar @$INSTALLDIR/lib/lib.rsp

if [ "x$GOMA_SERVICE" != "xtrue" ]; then
    ar rcsD $INSTALLDIR/lib/libc++abi.a $OUTDIR/obj/buildtools/third_party/libc++abi/libc++abi/*.o
    ar rcsD $INSTALLDIR/lib/libc++.a $OUTDIR/obj/buildtools/third_party/libc++/libc++/*.o
fi

cp -ar $OUTDIR/gen/* $INSTALLDIR/include
