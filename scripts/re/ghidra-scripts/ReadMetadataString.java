import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;

public class ReadMetadataString extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102f1b7c7L);
        byte[] b = new byte[0x31];
        for (int i = 0; i < 0x31; i++) {
            b[i] = getByte(addr.add(i));
        }
        println("Metadata format: " + new String(b));
    }
}
