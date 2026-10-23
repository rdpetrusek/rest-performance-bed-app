// Ghidra headless post-script: decompiles a fixed list of functions (found
// via FindProfileRefs.java) and prints their C pseudocode.
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Function;

import java.util.Arrays;
import java.util.List;

public class DumpFunctions extends GhidraScript {
    @Override
    public void run() throws Exception {
        List<String> addrs = Arrays.asList(
            "1f00abd4", "1f00aecc", "1f00af60", "1f00aff4",
            "1f002b64", "1f00692c"
        );

        DecompInterface decomp = new DecompInterface();
        decomp.openProgram(currentProgram);

        for (String a : addrs) {
            Address addr = currentProgram.getAddressFactory().getDefaultAddressSpace().getAddress(a);
            Function func = currentProgram.getFunctionManager().getFunctionContaining(addr);
            if (func == null) {
                println("No function at " + a);
                continue;
            }
            println("=== " + func.getName() + " @ " + func.getEntryPoint() + " ===");
            DecompileResults res = decomp.decompileFunction(func, 60, monitor);
            if (res != null && res.decompileCompleted()) {
                println(res.getDecompiledFunction().getC());
            } else {
                println("decompile failed: " + (res != null ? res.getErrorMessage() : "null result"));
            }
        }
        decomp.dispose();
    }
}
