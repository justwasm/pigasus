import ghidra.app.script.GhidraScript;
import ghidra.program.model.symbol.Reference;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Function;

public class FindXrefs extends GhidraScript {
    public void run() throws Exception {
        Address addr1 = currentProgram.getAddressFactory().getAddress("0x10676b912");
        Address addr2 = currentProgram.getAddressFactory().getAddress("0x10676b952");
        println("Refs to GetForwardChannelUri (0x10676b912):");
        for (Reference ref : getReferencesTo(addr1)) {
            Address from = ref.getFromAddress();
            Function func = currentProgram.getFunctionManager().getFunctionContaining(from);
            println("  from " + from + " in " + (func != null ? func.getName() : "no-func"));
        }
        println("Refs to GetBackChannelUri (0x10676b952):");
        for (Reference ref : getReferencesTo(addr2)) {
            Address from = ref.getFromAddress();
            Function func = currentProgram.getFunctionManager().getFunctionContaining(from);
            println("  from " + from + " in " + (func != null ? func.getName() : "no-func"));
        }
    }
}
