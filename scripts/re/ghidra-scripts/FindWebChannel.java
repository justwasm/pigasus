import ghidra.app.script.GhidraScript;
import ghidra.program.model.symbol.*;
import ghidra.program.model.listing.*;

public class FindWebChannel extends GhidraScript {
    @Override
    public void run() throws Exception {
        FunctionIterator iter = currentProgram.getFunctionManager().getFunctions(true);
        while (iter.hasNext()) {
            Function f = iter.next();
            String name = f.getName();
            if (name.toLowerCase().contains("webchannel")) {
                println(String.format("0x%x: %s", f.getEntryPoint().getOffset(), name));
            }
        }
    }
}
