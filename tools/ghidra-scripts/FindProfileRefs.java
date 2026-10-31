// Ghidra headless post-script: finds defined strings matching known markers
// from the profile/air control path and prints every code location that
// references each one, with its containing function - see tools/ghidra/run.sh.
import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.listing.Data;
import ghidra.program.model.listing.Function;
import ghidra.program.model.symbol.Reference;
import ghidra.program.model.symbol.ReferenceIterator;

import java.util.Arrays;
import java.util.List;

public class FindProfileRefs extends GhidraScript {
    @Override
    public void run() throws Exception {
        List<String> targets = Arrays.asList(
            "/air/filling", "/air/flows", "filling", "flows"
        );

        for (Data data : currentProgram.getListing().getDefinedData(true)) {
            Object val = data.getValue();
            if (val == null) {
                continue;
            }
            String sval = val.toString();
            boolean matched = false;
            for (String t : targets) {
                if (sval.contains(t)) {
                    matched = true;
                    break;
                }
            }
            if (!matched) {
                continue;
            }

            Address addr = data.getAddress();
            println("STRING @ " + addr + ": " + sval);
            ReferenceIterator refs = currentProgram.getReferenceManager().getReferencesTo(addr);
            boolean any = false;
            for (Reference ref : refs) {
                any = true;
                Address fromAddr = ref.getFromAddress();
                Function func = currentProgram.getFunctionManager().getFunctionContaining(fromAddr);
                String fname = (func != null) ? func.getName() : "??";
                println("  xref from " + fromAddr + " in function " + fname);
            }
            if (!any) {
                println("  (no xrefs found)");
            }
        }
    }
}
