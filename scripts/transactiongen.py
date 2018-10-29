#!/usr/bin/env python3

from datagen import error_and_exit
from enum import Enum
import numpy as np
import sys, datagen

"""
    File used to generate a series of transactions to test Trillium on.
    Each transaction refers to either a read or a write. A read leads
    to only reading from a single leaf node while a write specifies a
    user and public key pair and updates all the leaves containing that
    identifier.
"""

NUM_TRANSACTIONS = 1000 #Number of transactions to run at a given time.


"""
    Enum class used to distinguish between transactions which READ
    and transactions which WRITE.
"""
class TransactionType (Enum):
    ERROR = 0
    READ = 1
    WRITE = 2

CURR_STATE = TransactionType.READ #Variable that tells which Markov state the
                                  #system is currently in. The two states are
                                  #read in which the most recent transaction
                                  #was a read and makes further reads more
                                  #likely or a write which allows for
                                  #simulating bursty write behavior.

WRITE_PROBABILITY =  [0.1, 0.7]  #Percentage of transactions that should be
                                 #writes for each of the two states. The first
                                 #state is the read state and the second is
                                 #the write state.

def get_write_probability ():
    return WRITE_PROBABILITY [CURR_STATE.value - 1]

def update_state (state):
    CURR_STATE = state

"""
    Class used to hold the basic information for a transaction.
"""
class Transaction:

    """
        Constructor used to generate a random transaction for a tree.
    """
    def __init__ (self, t=None):
        self.type = TransactionType.ERROR
        if t is not None:
            self.tid = t.get_id ()
            self.type = TransactionType.ERROR
            self.set_transaction_type ()
            self.user, self.oldpk, self.id = t.get_random_leaf ()
            if self.type == TransactionType.WRITE:
                self.newpk = datagen.random_64s (datagen.KEY_SIZE)
                t.update_pk (self.user, self.oldpk, self.newpk)
            else:
                self.newpk = ""

    """
        Constructor used to generate a write transaction for a particular leaf.
        This transaction is associated with creating a leaf.
    """
    def configure_write_transaction (self, tid, user_id, pk, identifier):
        self.tid = tid
        self.user = user_id
        self.oldpk = ""
        self.id = identifier
        self.type = TransactionType.WRITE
        self.newpk = pk


    """
        Prints a transaction in form that can be read by a user.
    """
    def print_transaction (self):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            if self.type == TransactionType.READ:
                print ("Read to: ", end="")
            else:
                print ("Write to: ", end="")
            print ("(User: {}, with Public Key: {}, and Identifier: {})".format (self.user, self.pk, self.id))

    """
        Prints a transaction in a form that can be viewed as a database table.
        The table is a form of comma separated values in the form:

                       TYPE | TID | USER | OLDPK | IDENTIFIER | NEWPK

        where the TYPE is 1 for read and 2 for write.

        OLDPK is "" for any write which creates a leaf.

        NEWPK is "" for all reads.
    """
    def print_transaction_as_row (self):
        if self.type == TransactionType.ERROR:
            error_and_exit ("Invalid transaction type.")
        else:
            print ("{},{},{},{},{},{}".format (self.type.value, self.tid, self.user, self.oldpk, self.id, self.newpk))



    """
        Generates a random number and decides if the transaction
        is a read or a write.
    """
    def set_transaction_type (self):
        val = np.random.random ()
        prob = get_write_probability ()
        if val < prob:
            self.type = TransactionType.WRITE
        else:
            self.type = TransactionType.READ
        update_state (self.type)


"""
    Funciton used to generate a list of transactions for initializing the
    current tree t.
"""
def generate_init_transaction_list (t):
    trxns = []
    users = t.get_users ()
    for user in users:
        nodes = user.get_nodes ()
        for node in nodes:
            trxn = Transaction()
            trxn.configure_write_transaction (t.get_id (), user.get_id (), node.get_key (), node.get_id ())
            trxns.append (trxn)
    return trxns

"""
    Function used to generate a list of transactions to test concurrency on.
"""
def generate_transaction_list (t):
    return [Transaction (t) for c in range (NUM_TRANSACTIONS)]

"""
    Sample main function which will construct a sample set of transactions
    and print it.
"""
def main ():
    tid = None
    if len (sys.argv) > 2:
        error_and_exit ("This program takes at most 1 argument, the tree id.")
    elif len (sys.argv) == 2:
        tid = int (sys.argv[1])
    else:
        tid = datagen.random_64s (1)
    t = datagen.Tree (tid)
    init_trxns = generate_init_transaction_list (t)
    trxns = generate_transaction_list (t)
    for trxn in init_trxns:
        trxn.print_transaction_as_row ()
    for trxn in trxns:
        trxn.print_transaction_as_row ()

if __name__ == "__main__":
    main ()
