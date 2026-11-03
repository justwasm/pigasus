// TypeDesc.java — print the Go runtime type descriptor at the given addresses.
//
// A Go rtype is laid out as:
//   0x00  size       uintptr
//   0x08  ptrdata    uintptr
//   0x10  hash       uint32
//   0x14  tflag      uint8
//   0x15  align      uint8
//   0x16  fieldAlign uint8
//   0x17  kind       uint8
//   0x18  equal      func   (pointer)
//   0x20  gcdata     *byte
//   0x28  str        nameOff int32
//   0x2c  ptrToThis  int32
//
// The name string lives in the module's type-name section; the str field is
// an offset into it. We print the raw fields plus the resolved name when the
// name offset can be resolved through the symbol table.
//
// Usage:
//   analyzeHeadless <proj> agy -process -noanalysis \
//     -scriptPath <dir> -postScript TypeDesc.java 0x109be0df0 0x109ad7bf8
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Symbol;

public class TypeDesc extends GhidraScript {
    @Override
    public void run() throws Exception {
        for (String a : getScriptArgs()) {
            String t = a.trim();
            if (t.isEmpty()) {
                continue;
            }
            Address addr = toAddr(Long.decode(t));
            Symbol sym = currentProgram.getSymbolTable().getPrimarySymbol(addr);
            long size = getLong(addr);
            long ptrdata = getLong(addr.add(8));
            int hash = getInt(addr.add(0x10));
            int tflag = getByte(addr.add(0x14)) & 0xff;
            int kind = getByte(addr.add(0x17)) & 0xff;
            int strOff = getInt(addr.add(0x28));
            int ptrToThis = getInt(addr.add(0x2c));
            println(String.format(
                "%s  sym=%s  size=%d (0x%x)  ptrdata=%d  hash=0x%x  tflag=%d  kind=%d  str=%d  ptrToThis=%d",
                t, sym != null ? sym.getName() : "<no sym>", size, size, ptrdata,
                hash, tflag, kind, strOff, ptrToThis));
            // Go string header is {ptr,len}; try to read the name via the
            // nameOff offset relative to the module's types section.
            try {
                long namePtr = getLong(addr.add(0x28));
                if (namePtr != 0) {
                    Address np = toAddr(namePtr);
                    long len = getLong(np.add(8));
                    println(String.format("   raw name ptr=0x%x len=%d", namePtr, len));
                }
            } catch (Exception e) {
                // best effort
            }
        }
    }
}
