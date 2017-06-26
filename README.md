APK repacker
=======

APK repacker is a tool that takes an apk file and adds a new file into the archive, then re-sign the apk. It achieves the same effect as the following commands:

```bash
# adds a file to origin.apk and results in new.apk

unzip origin.apk -d origin/
echo "1234" > /tmp/cpid
cp /tmp/cpid origin/
rm -rf origin/META-INF
cd origin
jar -cf new-unsigned.apk *
jarsigner -keystore test.keystore -signedjar new.apk new-unsigned.apk 'test'
```

But it makes some differences:

1. The origin apk is stored in OSS
2. The origin apk need not be downloaded to local disk during the repack process
3. The new apk is stored in OSS
4. The new apk need not be written to local disk before storing to OSS

So it uses little disk space and is very efficient.

## Example

```bash
# git clone repacker

cd repacker
go build -o repack *.go
./repack -cert-name ANDROIDR -cpid 12345678 -source rockuw/qq.apk -dest rockuw/qq2.apk -oss-ep http://oss-cn-hangzhou.aliyuncs.com -oss-id akid -oss-key aksecret -priv-pem /tmp/zip/test.pem -work-dir /tmp/zip
```

## Convert keystore

`jarsigner` takes a `.keystore` file as the source of RSA key, to convert it to golang recognizable `.pem`, we need the following lines:

```bash
keytool -importkeystore -srckeystore test.keystore -destkeystore test.p12 -deststoretype PKCS12
openssl pkcs12 -in test.p12 -nocerts -nodes -out test.pem
```

## How it works

TODO: add a figure here

1. The [zip format][zip-format] allows appending to zip files without rewrite the entire file
2. The [great zipmerge][zip-merge] makes appending to zip files as easy as a charm
3. The great design in [great zipmerge][zip-merge] makes using OSS as the storage backend possible
4. The great [OSS][oss] features like multipart/uploadPartCopy/getObjectByRange makes OSS as a perfect storage backend

[zip-format]: https://en.wikipedia.org/wiki/Zip_(file_format)
[zip-merge]: https://github.com/rsc/zipmerge
[oss]: https://www.aliyun.com/product/oss
