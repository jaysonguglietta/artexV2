"""Bounded DER policy checks for ARTEX's OpenSSL CMS encrypted backup chunks.

This is a format/algorithm allowlist, not a cryptographic implementation. OpenSSL
still authenticates/decrypts each chunk; the separately pinned signature must be
verified before any archive parsing. See RFC5083, RFC5084 and RFC4055.
"""

MAX_CHUNK_BYTES = (8 << 20) + (1 << 20)
MAX_CONTENT_BYTES = (8 << 20) + 9
_AUTH_ENVELOPED = bytes.fromhex('2a864886f70d0109100117')
_DATA = bytes.fromhex('2a864886f70d010701')
_OAEP = bytes.fromhex('2a864886f70d010107')
_MGF1 = bytes.fromhex('2a864886f70d010108')
_P_SPECIFIED = bytes.fromhex('2a864886f70d010109')
_SHA256 = bytes.fromhex('608648016503040201')
_AES256_GCM = bytes.fromhex('60864801650304012e')


def _reject():
    raise ValueError('Backup CMS chunk does not match the authenticated encryption profile')


class _DER:
    """Definite, canonical lengths, bounded nodes/depth; no ciphertext recursion."""
    def __init__(self, data, budget=None, depth=0):
        if depth > 12:
            _reject()
        self.data = data
        self.offset = 0
        self.budget = budget if budget is not None else [128]
        self.depth = depth

    def peek(self):
        return self.data[self.offset] if self.offset < len(self.data) else None

    def value(self, expected):
        self.budget[0] -= 1
        if self.budget[0] < 0 or self.offset + 2 > len(self.data):
            _reject()
        tag, length = self.data[self.offset:self.offset + 2]
        self.offset += 2
        if tag != expected or tag & 31 == 31:
            _reject()
        if length & 128:
            count = length & 127
            if not 1 <= count <= 4 or self.offset + count > len(self.data):
                _reject()
            encoded = self.data[self.offset:self.offset + count]
            if not encoded[0]:
                _reject()
            length = int.from_bytes(encoded, 'big')
            self.offset += count
            if length < 128:
                _reject()
        if length > len(self.data) - self.offset:
            _reject()
        result = self.data[self.offset:self.offset + length]
        self.offset += length
        return result

    def child(self, tag=0x30):
        return _DER(self.value(tag), self.budget, self.depth + 1)

    def done(self):
        if self.offset != len(self.data):
            _reject()

    def oid(self, expected):
        if self.value(0x06) != expected:
            _reject()

    def integer(self, expected):
        if self.value(0x02) != expected:
            _reject()


def _sha256(reader):
    algorithm = reader.child()
    algorithm.oid(_SHA256)
    if algorithm.peek() is not None:
        if algorithm.value(0x05):  # AlgorithmIdentifier permits absent or NULL.
            _reject()
    algorithm.done()
    reader.done()


def _issuer_and_serial(reader):
    identity = reader.child()
    name = identity.child()
    if len(name.data) > 8192:
        _reject()
    while name.peek() is not None:
        attributes = name.child(0x31)
        previous = None
        if attributes.peek() is None:
            _reject()
        while attributes.peek() is not None:
            start = attributes.offset
            attribute = attributes.child()
            oid = attribute.value(0x06)
            if not 0 < len(oid) <= 64:
                _reject()
            first = True
            for octet in oid:
                if first and octet == 0x80:
                    _reject()
                first = not bool(octet & 128)
            if not first:
                _reject()
            # Only primitive DirectoryString/IA5String forms used in X.509 names.
            tag = attribute.peek()
            if tag not in (0x0C, 0x12, 0x13, 0x14, 0x16, 0x1C, 0x1E):
                _reject()
            if len(attribute.value(tag)) > 1024:
                _reject()
            attribute.done()
            encoded = bytes(attributes.data[start:attributes.offset])
            if previous is not None and encoded < previous:
                _reject()
            previous = encoded
        attributes.done()
    name.done()
    serial = identity.value(0x02)
    if (not 0 < len(serial) <= 32 or serial[0] & 128
            or (len(serial) > 1 and serial[0] == 0 and not serial[1] & 128)):
        _reject()
    identity.done()


def _recipient(reader):
    recipient = reader.child()
    recipient.integer(b'\x00')
    _issuer_and_serial(recipient)
    algorithm = recipient.child()
    algorithm.oid(_OAEP)
    parameters = algorithm.child()
    _sha256(parameters.child(0xA0))  # Missing fields would select SHA1 defaults.
    mask = parameters.child(0xA1)
    mask_algorithm = mask.child()
    mask_algorithm.oid(_MGF1)
    _sha256(mask_algorithm)
    mask.done()
    if parameters.peek() is not None:
        label = parameters.child(0xA2)
        label_algorithm = label.child()
        label_algorithm.oid(_P_SPECIFIED)
        if label_algorithm.value(0x04):
            _reject()
        label_algorithm.done()
        label.done()
    parameters.done()
    algorithm.done()
    # The generated identity is RSA3072; allow larger RSA identities up to8192.
    if not 384 <= len(recipient.value(0x04)) <= 1024:
        _reject()
    recipient.done()
    reader.done()  # Exactly one recipient, not a SET containing additional keys.


def validate_cms_chunk(data):
    """Reject anything outside the bounded AuthEnvelopedData backup profile."""
    if not isinstance(data, (bytes, bytearray)) or not 0 < len(data) <= MAX_CHUNK_BYTES:
        _reject()
    root = _DER(memoryview(data))
    content_info = root.child()
    content_info.oid(_AUTH_ENVELOPED)
    explicit = content_info.child(0xA0)
    envelope = explicit.child()
    envelope.integer(b'\x00')
    _recipient(envelope.child(0x31))
    content = envelope.child()
    content.oid(_DATA)
    algorithm = content.child()
    algorithm.oid(_AES256_GCM)
    parameters = algorithm.child()
    if len(parameters.value(0x04)) != 12:
        _reject()
    parameters.integer(b'\x10')  # Explicit128-bit ICV; absent defaults to96 bits.
    parameters.done()
    algorithm.done()
    if not 9 <= len(content.value(0x80)) <= MAX_CONTENT_BYTES:
        _reject()
    content.done()
    if len(envelope.value(0x04)) != 16:
        _reject()
    envelope.done()  # No originator information or authenticated/unauthenticated attrs.
    explicit.done()
    content_info.done()
    root.done()
