package IRCSock;

# IRCSock.pm
# walkure at 3pf.jp

use strict;
use warnings;
use Encode;

use base qw/IO::Socket::INET/;
#use Data::Dumper;

sub configure
{
	my ( $self, $args ) = @_;
	
	*$self->{params} = $args->{PeerAddr};
	my $params = $args->{PeerAddr};
	$args->{PeerAddr} = $params->{host}.':'.$params->{port};
	*$self->{buffer} = '';
	*$self->{charset} = $params->{charset} || 'jis';

	*$self->{handlers} = {
		'001'		=> \&do_connected, #connected
		'002'		=> \&do_nothing,   #host
		'003'		=> \&do_nothing,   #server timestamp
		'004'		=> \&do_nothing,   #server mask
		'005'		=> \&do_nothing,   #RFC2812
		'020'		=> \&do_nothing,   #waiting
		'042'		=> \&do_nothing,   #unique ID
		'251'		=> \&do_nothing,   #users
		'252'		=> \&do_nothing,   #opers
		'253'		=> \&do_nothing,   #unknown connections
		'254'		=> \&do_nothing,   #channels
		'255'		=> \&do_nothing,   #lusers
		'265'		=> \&do_nothing,   #lusers2
		'266'		=> \&do_nothing,   #gusers2
		'372'		=> \&do_nothing,   #motd
		'375'		=> \&do_nothing,   #begin of motd
		'376'		=> \&do_nothing,   #end of motd
		'JOIN'		=> \&do_nothing,   #JOIN
		'NOTICE'	=> \&do_nothing,   #NOTICE
		'PRIVMSG'	=> \&do_nothing,   #PRIVMSG
		'NICK'		=> \&do_nothing,   #NICK
		'QUIT'		=> \&do_nothing,   #QUIT
		'MODE'		=> \&do_nothing,   #MODE
		'332'		=> \&do_nothing,   #channel topic
		'333'		=> \&do_nothing,   #channel topic owner
		'353'		=> \&do_nothing,   #channel member list
		'366'		=> \&do_nothing,   #end of channel member list
	};

	$self->SUPER::configure($args);
}

sub login
{
	my ($self,$channel) = @_;
	my $param = *$self->{params};
	
	*$self->{init_channels} = $channel;
	
	print $self "PASS $param->{password}\n" if $param->{password};
	print $self "NICK $param->{nick}\n";
	print $self "USER $param->{name} 0 * :$param->{desc}\n";

}

sub set_callback
{
	my($self,$action,$func) = @_;
	*$self->{handlers}{uc $action} = $func;
}

sub parse_body
{
	my($self,$body) = @_;
	
	$body =~ s/\x0D\x0A|\x0D|\x0A/\n/g;
	$body = *$self->{buffer} . $body if length *$self->{buffer};
	
	my $i;
	while (($i = index($body,"\n")) >= 0){ #index for the head of string is 0
		my $line = substr($body,0,$i);
		$body = substr($body,$i+1);
		$self->parse_line($line);
	}
	*$self->{buffer} = $body;
}

sub parse_line
{
	my($self,$line) = @_;
	
	$line = Encode::decode(*$self->{charset},$line);

	my $frag_index = index($line,' :');
	my $message = substr($line,1,$frag_index) ;
	my $fragment = substr($line,$frag_index+2) ;

	my @params = split(/\s/,$message);
	my $sender = shift @params;
	my $type   = shift @params;
	my $target = shift @params;
	my $args   = join(' ',@params) || '';

	if(uc($sender) eq 'ING'){
		print $self "PONG :$fragment\n";
#		print "PINGED.\n";
		return;
	}
	
	#call handler
	if(defined *$self->{handlers}{uc $type}){
		*$self->{handlers}{uc $type}->($self,$line,$sender,$target,$args,$fragment);
	}else{
		print "[$line]($sender/$type/$target/$args/$fragment)\n";
	}
}

sub do_nothing
{
	my($self,$line,$sender,$target,$args,$fragment) = @_;
#	print "[$line]\n";
}

sub do_connected
{
	my($self,$line,$sender,$target,$args,$fragment) = @_;

#	print "[CONNECT][$line][$sender][$target][$args][$fragment]\n";
	return unless defined *$self->{init_channels};
	foreach(@{*$self->{init_channels}}){
		print $self "JOIN $_\n";
#		print "++Joining $_\n";
	}
}

sub privmsg
{
	my($self,$target,$message) = @_;

	$message = Encode::encode(*$self->{charset},$message);
	print $self "PRIVMSG $target :$message\n";
}

sub notice
{
	my($self,$target,$message) = @_;

	$message = Encode::encode(*$self->{charset},$message);
	print $self "NOTICE $target :$message\n";
}

1;
