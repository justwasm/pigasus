import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.symbol.Reference;

public class FindStringRefs extends GhidraScript {
    @Override
    public void run() throws Exception {
        Address addr = toAddr(0x102ea25c8L); // address of /v1internal:connectInstanceV2
        Reference[] refs = getReferencesTo(addr);
        for (Reference r : refs) {
            println(String.format("Xref from 0x%x to /v1internal:connectInstanceV2", r.getFromAddress().getOffset()));
        }
    }
}
