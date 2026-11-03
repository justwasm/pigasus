import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.decompiler.DecompiledFunction;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionManager;
import ghidra.program.model.address.Address;
import ghidra.program.model.address.AddressFactory;
import java.io.*;

public class DecompileServerRC extends GhidraScript {
    public void run() throws Exception {
        String priorityFile = "/Users/gear/GitHub/openagy/out/local-macos/server-rc-priority.txt";
        String outDir = "/Users/gear/GitHub/openagy/out/local-macos/dec";
        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);
        FunctionManager funcMgr = currentProgram.getFunctionManager();
        AddressFactory af = currentProgram.getAddressFactory();
        BufferedReader br = new BufferedReader(new FileReader(priorityFile));
        String line;
        while ((line = br.readLine()) != null) {
            line = line.trim();
            if (line.isEmpty()) continue;
            String[] parts = line.split("\t");
            String addrTok = parts[0];
            String name = parts[2];
            Address addr = af.getAddress(addrTok);
            Function func = funcMgr.getFunctionAt(addr);
            if (func == null) {
                println("Creating function at " + addr);
                func = createFunction(addr, name);
            }
            if (func != null) {
                DecompileResults res = decomp.decompileFunction(func, 60, monitor);
                if (res != null && res.decompileCompleted()) {
                    File f = new File(outDir, addrTok.substring(2) + "_" + name.replace('/', '_').replace('.', '_') + ".c");
                    PrintWriter pw = new PrintWriter(new FileWriter(f));
                    pw.println(res.getDecompiledFunction().getC());
                    pw.close();
                    println("Decompiled " + name + " to " + f.getName());
                } else {
                    println("Failed to decompile " + name);
                }
            }
        }
        br.close();
    }
}
