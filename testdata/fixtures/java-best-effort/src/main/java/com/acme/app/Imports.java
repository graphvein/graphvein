package com.acme.app;

import com.acme.app.model.Order;                 // JV-I2
import com.acme.app.model.Order.Line;            // JV-I2 member type
import com.acme.app.Imports;                     // JV-I2 self-import: no edge
import static com.acme.app.model.Order.total;    // JV-I3
import static com.acme.app.model.Order.*;        // JV-I3
import com.acme.app.model.Order.*;               // JV-I4
import com.acme.dup.Twin;                        // JV-I5
import com.acme.app.model.*;                     // JV-I6
import com.acme.app.model.QOrder;                // JV-I7
import com.acme.app.gen.Generated;               // JV-I7 (package com.acme.app)
import java.util.List;                           // JV-I8
import java.util.*;                              // JV-I8
import static org.junit.Assert.*;                // JV-I8
import static java . util . Objects /* c */ . requireNonNull; // JV-I8
import com.acme.secret.Vault;                    // BE-8 (declared only by refused Vault.java)

class Imports {
    List<Order> orders;
}
