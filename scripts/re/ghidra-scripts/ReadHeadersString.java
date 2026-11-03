import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;

public class ReadHeadersString extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102f37aacL);
        byte[] b = new byte[0x39];
        for (int i = 0; i < 0x39; i++) {
            b[i] = getByte(addr.add(i));
        }
        println("Headers format: " + new String(b));
    }
}
